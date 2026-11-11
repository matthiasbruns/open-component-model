package digest

import (
	"crypto"
	"fmt"
	"slices"
	"strings"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Complete returns the digest to record for a resource whose digest an author wrote in a
// component constructor, given the digest the binding resolved and the rule for that content.
//
// Such a digest belongs to no descriptor yet: nothing signs it and no parent reference covers
// it, so a field the author left out is taken from what the binding resolved, and the spelling
// the author chose is canonicalised. What the author did pin is checked, ignoring case and the
// dash in a hash name, and only a genuine conflict is an error: a different hash algorithm, a
// normalisation the rule does not describe, or a different value.
//
// An excluded resource is returned untouched and computed is not used, as in [Check].
//
// Complete is the lenient half of the rules and runs once, while a component version is built.
// Every later step sees a recorded digest and goes through [Check], which fills nothing in.
func Complete(authored *descriptor.Digest, computed Computed, rule Rule) (*descriptor.Digest, error) {
	if IsExcluded(authored) {
		return authored.DeepCopy(), nil
	}

	completed, err := generate(computed, rule)
	if err != nil {
		return nil, err
	}
	if authored == nil {
		return completed, nil
	}

	if err := CheckPinned(authored, computed.Hash, rule); err != nil {
		return nil, err
	}
	if value := authored.Value; value != "" && !namesValue(value, computed) {
		return nil, fmt.Errorf("digest value mismatch: expected %s, got %s", authored.Value, computed.Value)
	}

	return completed, nil
}

// CheckPinned reports a conflict between what an author pinned in a component constructor and
// what a binding is going to compute, as far as that can be judged without the content: the hash
// algorithm and the normalisation. A binding that has to fetch content to compute a digest calls
// it before the download, so a constructor entry it can never satisfy costs nothing.
//
// [Complete] applies the same two checks, so a caller holding the content needs only Complete.
func CheckPinned(authored *descriptor.Digest, hash crypto.Hash, rule Rule) error {
	if IsExcluded(authored) || authored == nil {
		return nil
	}
	if name := authored.HashAlgorithm; name != "" {
		pinned, err := algorithms.Hash(name)
		if err != nil {
			return err
		}
		if pinned != hash {
			return fmt.Errorf("hash algorithm mismatch: expected %s, got %s", name, hash)
		}
	}
	if label := authored.NormalisationAlgorithm; label != "" && !rule.describes(label) {
		return fmt.Errorf("%w: expected %s, got %s", ErrNormalisationMismatch, rule.Normalisation, label)
	}
	return nil
}

// describes is [Rule.accepts] for a label an author wrote, whose spelling is not fixed yet.
func (r Rule) describes(normalisation string) bool {
	if strings.EqualFold(normalisation, r.Normalisation) {
		return true
	}
	return slices.ContainsFunc(r.Accepted, func(accepted string) bool {
		return strings.EqualFold(accepted, normalisation)
	})
}

// namesValue reports whether an author-written value names the computed digest. Case is ignored,
// and an "<algorithm>:<hex>" prefix, as a digest prints itself, is accepted when it names the
// algorithm that was computed.
func namesValue(value string, computed Computed) bool {
	if prefix, encoded, prefixed := strings.Cut(value, ":"); prefixed {
		if hash, err := algorithms.Hash(prefix); err != nil || hash != computed.Hash {
			return false
		}
		value = encoded
	}
	return strings.EqualFold(value, computed.Value)
}
