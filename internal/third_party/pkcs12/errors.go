// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkcs12

import "errors"

var (
	// ErrDecryption represents a failure to decrypt the input.
	ErrDecryption = errors.New("pkcs12: decryption error, incorrect padding")

	// ErrIncorrectPassword is returned when an incorrect password is detected.
	// Usually, P12/PFX data is signed to be able to verify the password.
	ErrIncorrectPassword = errors.New("pkcs12: decryption password incorrect")

	// ErrTooManyIterations is returned when a KDF in the file asks for
	// more than MaxIterations rounds. The count is read from the file,
	// so without a ceiling a crafted file pins a core for as long as
	// it likes.
	ErrTooManyIterations = errors.New("pkcs12: key derivation iteration count above the limit")
)

// MaxIterations is the ceiling on every password-based KDF a file
// being DECODED may ask for: the MAC, each encrypted safe and each
// shrouded key bag. Tools write a few thousand, hardened ones a few
// hundred thousand, so a million is above anything a real file
// carries. The encoder is not bound by it.
const MaxIterations = 1 << 20

// checkIterations refuses a KDF round count above MaxIterations.
func checkIterations(n int) error {
	if n > MaxIterations {
		return ErrTooManyIterations
	}

	return nil
}

// NotImplementedError indicates that the input is not currently supported.
type NotImplementedError string

func (e NotImplementedError) Error() string {
	return "pkcs12: " + string(e)
}
