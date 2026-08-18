// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package blob

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
)

// Load returns the decoded bytes of one attachment, whether it lives
// inline in the database or in the object store.
//
// Three call sites had their own copy of this switch - outbound
// email, template attachments, inbound mail - and two of them tested
// the two cases in opposite order, so a row that somehow carried both
// an inline body and a storage key would resolve differently
// depending on which endpoint you asked. One order, decided here:
// storage key wins.
//
// The key is authoritative because offloading is the operation that
// clears Content, so a row holding both is a half-finished offload
// whose inline copy is the stale one. Name only appears in errors.
func Load(ctx context.Context, store Store, storageKey, inlineBase64, name string) ([]byte, error) {
	if storageKey != "" {
		if store == nil {
			return nil, fmt.Errorf("attachment %q is offloaded but no blob store is configured", name)
		}

		rc, err := store.Get(ctx, storageKey)
		if err != nil {
			return nil, err
		}

		defer func() { _ = rc.Close() }()

		return io.ReadAll(rc)
	}

	if inlineBase64 != "" {
		raw, err := base64.StdEncoding.DecodeString(inlineBase64)
		if err != nil {
			return nil, fmt.Errorf("attachment %q has invalid base64 content: %w", name, err)
		}

		return raw, nil
	}

	return nil, fmt.Errorf("attachment %q has no content", name)
}
