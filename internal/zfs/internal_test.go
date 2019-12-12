package zfs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNestedTransaction(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		parentCancel bool
		nestedFail   bool
	}{
		"Done with no error": {},

		"Nested failed, parent pass":     {nestedFail: true},
		"Parent canceled, nested pass":   {parentCancel: true},
		"Parent canceled, nested failed": {nestedFail: true, parentCancel: true},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			z, err := New(context.Background())
			if err != nil {
				t.Fatalf("couldn't create base ZFS object: %v", err)
			}

			var resultFromParent, resultFromNested string

			trans, cancel := z.NewTransaction(context.Background())
			trans.registerRevert(func() error {
				resultFromParent = "reverted from parent"
				return nil
			})

			assert.Equal(t, 1, len(trans.reverts), "parent transaction should have one pending revert")

			nested := trans.newNestedTransaction()
			nested.registerRevert(func() error {
				resultFromNested = "reverted from nested"
				return nil
			})

			assert.Equal(t, 1, len(nested.reverts), "nested transaction should have one pending revert")

			var nestedErr error
			if tc.nestedFail {
				nestedErr = errors.New("failure")
			}
			nested.Done(&nestedErr)

			if tc.nestedFail {
				assert.Equal(t, "reverted from nested", resultFromNested, "revert should be called on nested")
				assert.Equal(t, 1, len(trans.reverts), "parent transaction should still have one pending revert")
			} else {
				assert.Equal(t, "", resultFromNested, "nested transaction is committed")
				assert.Equal(t, 2, len(trans.reverts), "parent transaction should now have 2 pending reverts (parent + nested)")
			}
			assert.Equal(t, 0, len(nested.reverts), "nested transaction should have no more pending revert")

			if tc.parentCancel {
				cancel()
				<-trans.done
				assert.Equal(t, "reverted from nested", resultFromNested, "revert created on nested should be called from parent")
				assert.Equal(t, "reverted from parent", resultFromParent, "revert created on parent should be called from parent")
				trans.Done()
				assert.Equal(t, "reverted from nested", resultFromNested, "done after fail is a no-op on nested")
				assert.Equal(t, "reverted from parent", resultFromParent, "done after fail is a no-op on parent")
			} else {
				trans.Done()
				if tc.nestedFail {
					assert.Equal(t, "reverted from nested", resultFromNested, "nested has failed")
				} else {
					assert.Equal(t, "", resultFromNested, "no revert from nested transaction")
				}
				assert.Equal(t, "", resultFromParent, "no revert from parent transaction")
			}

			assert.Equal(t, 0, len(nested.reverts), "nested transaction should have no more pending revert")
			assert.Equal(t, 0, len(trans.reverts), "parent transaction should have no more pending revert")

		})
	}
}

func TestNestedTransactionParentCancelledBeforeDone(t *testing.T) {
	t.Parallel()

	z, err := New(context.Background())
	if err != nil {
		t.Fatalf("couldn't create base ZFS object: %v", err)
	}

	var resultFromParent, resultFromNested string

	trans, cancel := z.NewTransaction(context.Background())
	trans.registerRevert(func() error {
		resultFromParent = "reverted from parent"
		return nil
	})

	assert.Equal(t, 1, len(trans.reverts), "parent transaction should have one pending revert")

	nested := trans.newNestedTransaction()
	nested.registerRevert(func() error {
		resultFromNested = "reverted from nested"
		return nil
	})

	assert.Equal(t, 1, len(nested.reverts), "nested transaction should have one pending revert")

	// cancel parent
	cancel()
	<-trans.done

	assert.Equal(t, "reverted from nested", resultFromNested, "revert created on nested should be called from parent")
	assert.Equal(t, "reverted from parent", resultFromParent, "revert created on parent should be called from parent")

	var errNested error
	nested.Done(&errNested)
	trans.Done()
	assert.Equal(t, "reverted from nested", resultFromNested, "done after fail is a no-op on nested")
	assert.Equal(t, "reverted from parent", resultFromParent, "done after fail is a no-op on parent")

}
