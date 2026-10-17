// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mailsign

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

const (
	pgpProtocol = "application/pgp-signature"
	pgpMicAlg   = "pgp-sha256"

	armorPrivate = "PGP PRIVATE KEY BLOCK"
	armorPublic  = "PGP PUBLIC KEY BLOCK"
)

// pgpConfig is the one profile every key here is made and used with:
// Ed25519 for signing, Curve25519 for the encryption subkey, SHA-256
// for every hash. RSA is accepted on import because that is what most
// existing keys are.
func pgpConfig() *packet.Config {
	return &packet.Config{
		Algorithm:   packet.PubKeyAlgoEdDSA,
		Curve:       packet.Curve25519,
		DefaultHash: crypto.SHA256,
	}
}

// GeneratePGP mints a key whose single user id is the sender.
func GeneratePGP(name, email string) (Material, error) {
	// The user id is "Name <email>" and go-crypto refuses the
	// characters that would break that form. A display name is free
	// text, so it is cleaned rather than refused.
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune("()<>\x00", r) {
			return -1
		}

		return r
	}, name)
	entity, err := openpgp.NewEntity(name, "", email, pgpConfig())
	if err != nil {
		return Material{}, fmt.Errorf("mailsign: generate pgp key: %w", err)
	}

	return pgpMaterial(entity)
}

// ImportPGP takes an armored private key, opens it with passphrase
// when it is protected, and keeps it only if one of its user ids is
// the sender address - a key for somebody else would sign as them.
func ImportPGP(armored, passphrase, email string) (Material, error) {
	entity, err := readPGPPrivate(armored)
	if err != nil {
		return Material{}, err
	}

	// Every refusal that needs no passphrase runs before a derivation.
	if !pgpNamesAddress(entity, email) {
		return Material{}, fmt.Errorf("mailsign: the key carries no user id for %s", email)
	}

	if err := checkPGPProtection(armored); err != nil {
		return Material{}, err
	}

	// Opens the primary and every subkey that is protected, and does
	// nothing for a key with none.
	if err := entity.DecryptPrivateKeys([]byte(passphrase)); err != nil {
		return Material{}, ErrPassphrase
	}

	if _, ok := entity.SigningKey(time.Now()); !ok {
		return Material{}, errors.New("mailsign: the key has no usable signing key, it may be expired or revoked")
	}

	return pgpMaterial(entity)
}

// The passphrase derivation of one import is bounded, and every bound
// is read from the key before anything is derived. go-crypto derives
// once per protected packet, the primary and each subkey, so a packet
// count and a total stand beside the per-packet bounds. A key is
// imported by any project member, so these are what keep one of them
// from taking the node down.
const (
	// maxArgon2MemoryExp is 2^16 KiB, 64 MiB, the memory-constrained
	// profile of RFC 9580. go-crypto accepts up to 2^31 KiB and
	// allocates it in one piece before the first pass.
	maxArgon2MemoryExp = 16

	// maxArgon2Passes admits the three passes RFC 9580 recommends.
	maxArgon2Passes = 4

	// maxPGPProtectedPackets is the primary plus a handful of subkeys.
	maxPGPProtectedPackets = 8

	// maxPGPDerivationWork is the bytes one import may run through its
	// derivations, four passes over 64 MiB. Argon2 counts passes times
	// its memory, the iterated S2K the octets it hashes.
	maxPGPDerivationWork = maxArgon2Passes << (maxArgon2MemoryExp + 10)
)

// ErrKeyProtection is a protected key whose passphrase derivation asks
// for more than an import may spend.
var ErrKeyProtection = errors.New("mailsign: the key's passphrase protection is heavier than an import may run, export it with a lighter protection or without a passphrase")

// Secret key packet tags, RFC 9580 section 5.5.1.3 and 5.5.1.4.
const (
	tagSecretKey    = 5
	tagSecretSubkey = 7
)

// S2K specifier modes that cost more than one hash, RFC 9580 3.7.1.
const (
	s2kIterated = 3
	s2kArgon2   = 4
)

// checkPGPProtection reads the string-to-key parameters of every
// secret key packet in the block and refuses a key whose derivations
// exceed the bounds above, per packet and in total. go-crypto keeps
// those parameters to itself, so they are read off the packet bytes:
// the public portion is sized by serializing it, and what follows is
// the S2K usage octet, the cipher, the AEAD mode where usage is 253,
// the v6 length octet, then the S2K specifier. Anything that cannot be
// read is refused, never opened: a key this cannot parse is not one
// the decrypt may run on.
func checkPGPProtection(armored string) error {
	block, err := armor.Decode(strings.NewReader(armored))
	if err != nil {
		return fmt.Errorf("mailsign: not an armored openpgp key: %w", err)
	}

	var packets, work int64
	r := packet.NewOpaqueReader(block.Body)
	for {
		op, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("mailsign: read key packet: %w", err)
		}

		if op.Tag != tagSecretKey && op.Tag != tagSecretSubkey {
			continue
		}

		encrypted, cost, err := secretKeyPacketCost(op)
		if err != nil {
			return err
		}

		if !encrypted {
			continue
		}

		packets++
		work += cost
		if packets > maxPGPProtectedPackets || work > maxPGPDerivationWork {
			return ErrKeyProtection
		}
	}
}

// secretKeyPacketCost reports whether a secret key packet is protected
// and the bytes its derivation runs through.
func secretKeyPacketCost(op *packet.OpaquePacket) (bool, int64, error) {
	parsed, err := op.Parse()
	if err != nil {
		return false, 0, fmt.Errorf("mailsign: parse secret key packet: %w", err)
	}

	pk, ok := parsed.(*packet.PrivateKey)
	if !ok {
		return false, 0, errors.New("mailsign: secret key packet did not parse as one")
	}

	if !pk.Encrypted {
		return false, 0, nil
	}

	// The public portion as the packet carries it, measured through
	// the signature-hash form, which is a fixed prefix plus that body.
	var prefix, hashed bytes.Buffer
	if err := pk.SerializeSignaturePrefix(&prefix); err != nil {
		return false, 0, err
	}

	if err := pk.SerializeForHash(&hashed); err != nil {
		return false, 0, err
	}

	rest := op.Contents
	i := hashed.Len() - prefix.Len()
	if i < 0 || i >= len(rest) {
		return false, 0, ErrKeyProtection
	}

	usage := rest[i]
	i++

	// A v5 or v6 packet carries a count of the optional fields next.
	if pk.Version >= 5 {
		i++
	}

	// The cipher, then the AEAD mode when usage is 253.
	i++
	if usage == 253 {
		i++
	}

	// A v6 packet carries the length of the S2K specifier.
	if pk.Version == 6 {
		i++
	}

	if i >= len(rest) {
		return false, 0, ErrKeyProtection
	}

	// The specifier opens with its mode. The iterated mode follows it
	// with a hash id, an 8 octet salt and the encoded count, Argon2
	// with a 16 octet salt, passes, parallelism and the memory exponent.
	const iteratedCountOffset = 1 + 1 + 8
	const argon2PassesOffset, argon2MemoryOffset = 1 + 16, 1 + 16 + 1 + 1
	switch rest[i] {
	case s2kIterated:
		if i+iteratedCountOffset >= len(rest) {
			return false, 0, ErrKeyProtection
		}

		c := int64(rest[i+iteratedCountOffset])

		return true, (16 + c&15) << (c>>4 + 6), nil
	case s2kArgon2:
		if i+argon2MemoryOffset >= len(rest) {
			return false, 0, ErrKeyProtection
		}

		passes, memoryExp := rest[i+argon2PassesOffset], rest[i+argon2MemoryOffset]
		if passes > maxArgon2Passes || memoryExp > maxArgon2MemoryExp {
			return false, 0, ErrKeyProtection
		}

		return true, int64(passes) << (memoryExp + 10), nil
	}

	return true, 0, nil
}

// readPGPPrivate parses an armored key ring and picks the first entity
// that carries a private key.
func readPGPPrivate(armored string) (*openpgp.Entity, error) {
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil {
		return nil, fmt.Errorf("mailsign: not an armored openpgp key: %w", err)
	}

	for _, e := range ring {
		if e.PrivateKey != nil {
			return e, nil
		}
	}

	return nil, errors.New("mailsign: the block holds no private key")
}

func pgpNamesAddress(entity *openpgp.Entity, email string) bool {
	for _, id := range entity.Identities {
		if id.UserId != nil && strings.EqualFold(id.UserId.Email, email) {
			return true
		}
	}

	return false
}

// pgpMaterial serializes the entity both ways. The private half is
// written WITHOUT a passphrase: it is sealed at rest by the caller, and
// a passphrase on top would have to be stored beside it to be of use.
func pgpMaterial(entity *openpgp.Entity) (Material, error) {
	var priv bytes.Buffer
	w, err := armor.Encode(&priv, armorPrivate, nil)
	if err != nil {
		return Material{}, err
	}

	if err := entity.SerializePrivateWithoutSigning(w, nil); err != nil {
		return Material{}, fmt.Errorf("mailsign: serialize private key: %w", err)
	}

	if err := w.Close(); err != nil {
		return Material{}, err
	}

	pub, err := armorPublicKey(entity)
	if err != nil {
		return Material{}, err
	}

	return Material{Kind: KindPGP, Private: priv.String(), Public: pub}, nil
}

func armorPublicKey(entity *openpgp.Entity) (string, error) {
	var pub bytes.Buffer
	w, err := armor.Encode(&pub, armorPublic, nil)
	if err != nil {
		return "", err
	}

	if err := entity.Serialize(w); err != nil {
		return "", fmt.Errorf("mailsign: serialize public key: %w", err)
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	return pub.String(), nil
}

// pgpSigner signs with one entity.
type pgpSigner struct {
	entity *openpgp.Entity
}

func newPGPSigner(armored string) (Signer, error) {
	entity, err := readPGPPrivate(armored)
	if err != nil {
		return nil, err
	}

	if entity.PrivateKey.Encrypted {
		return nil, errors.New("mailsign: the stored pgp key is passphrase protected")
	}

	return &pgpSigner{entity: entity}, nil
}

func (s *pgpSigner) Protocol() string { return pgpProtocol }
func (s *pgpSigner) MicAlg() string   { return pgpMicAlg }

// Sign writes a binary detached signature over the entity as given.
// RFC 3156 has the signer hash the CRLF form the entity travels in,
// and the builder has already written it that way, so no text mode
// canonicalization is layered on top.
func (s *pgpSigner) Sign(entity []byte) (Part, error) {
	var sig bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&sig, s.entity, bytes.NewReader(entity), pgpConfig()); err != nil {
		return Part{}, fmt.Errorf("mailsign: pgp sign: %w", err)
	}

	return Part{
		ContentType:      pgpProtocol + `; name="signature.asc"`,
		Filename:         "signature.asc",
		TransferEncoding: "7bit",
		Body:             sig.Bytes(),
	}, nil
}

// describePGP reads an armored public key.
func describePGP(armored string) (Description, error) {
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil || len(ring) == 0 {
		return Description{}, fmt.Errorf("mailsign: not an armored openpgp key: %w", err)
	}

	entity := ring[0]
	d := Description{
		Kind:        KindPGP,
		Fingerprint: strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint)),
		Algorithm:   pgpAlgorithm(entity.PrimaryKey),
	}
	if id := entity.PrimaryIdentity(); id != nil {
		d.Subject = id.Name
	}

	// The expiry lives on the self signature, not on the key packet.
	if sig, _ := entity.PrimarySelfSignature(); sig != nil && sig.KeyLifetimeSecs != nil && *sig.KeyLifetimeSecs > 0 {
		expires := entity.PrimaryKey.CreationTime.Add(time.Duration(*sig.KeyLifetimeSecs) * time.Second)
		d.NotAfter = &expires
	}

	return d, nil
}

func pgpAlgorithm(pk *packet.PublicKey) string {
	switch pk.PubKeyAlgo {
	case packet.PubKeyAlgoEdDSA, packet.PubKeyAlgoEd25519:
		return "Ed25519"
	case packet.PubKeyAlgoEd448:
		return "Ed448"
	case packet.PubKeyAlgoECDSA:
		return "ECDSA"
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSASignOnly:
		if bits, err := pk.BitLength(); err == nil {
			return fmt.Sprintf("RSA-%d", bits)
		}

		return "RSA"
	default:
		return fmt.Sprintf("algorithm %d", pk.PubKeyAlgo)
	}
}

// AutocryptKeyData is the base64 the Autocrypt header carries: the
// public key reduced to what a client needs to encrypt back - primary
// key, the primary user id, and the subkeys. Third-party certifications
// and every other user id are left out, because the header goes on
// every message and a key that collected signatures at a keysigning
// party would add kilobytes to each.
func AutocryptKeyData(armored string) (string, error) {
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil || len(ring) == 0 {
		return "", fmt.Errorf("mailsign: not an armored openpgp key: %w", err)
	}

	full := ring[0]
	minimal := &openpgp.Entity{
		PrimaryKey: full.PrimaryKey,
		Subkeys:    full.Subkeys,
		Identities: map[string]*openpgp.Identity{},
	}
	if id := full.PrimaryIdentity(); id != nil {
		minimal.Identities[id.Name] = &openpgp.Identity{
			Name:          id.Name,
			UserId:        id.UserId,
			SelfSignature: id.SelfSignature,
			Signatures:    []*packet.Signature{id.SelfSignature},
		}
	}

	var raw bytes.Buffer
	if err := minimal.Serialize(&raw); err != nil {
		return "", fmt.Errorf("mailsign: serialize autocrypt key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(raw.Bytes()), nil
}
