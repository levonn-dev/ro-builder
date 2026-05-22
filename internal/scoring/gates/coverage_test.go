package gates

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/data"
)

// fakeItemLookup implements catalogItemLookup so itemGrants and the
// equipment-walking helpers can be exercised without loading the
// embedded catalog. Justifies the interface's existence.
type fakeItemLookup map[int]data.Item

func (f fakeItemLookup) Item(id int) (data.Item, bool) {
	it, ok := f[id]
	return it, ok
}

func TestItemGrants_MissingItemReturnsFalse(t *testing.T) {
	fake := fakeItemLookup{}
	if itemGrants(fake, 1001, data.ImmunityStun) {
		t.Fatalf("itemGrants on missing id should return false")
	}
}

func TestItemGrants_NoMatchingImmunity(t *testing.T) {
	fake := fakeItemLookup{
		2103: {ID: 2103, GrantsImmunity: []string{data.ImmunitySleep}},
	}
	if itemGrants(fake, 2103, data.ImmunityStun) {
		t.Fatalf("itemGrants should return false when status not in GrantsImmunity")
	}
}

func TestItemGrants_MatchingImmunity(t *testing.T) {
	fake := fakeItemLookup{
		2115: {ID: 2115, GrantsImmunity: []string{data.ImmunityStun, data.ImmunitySleep}},
	}
	if !itemGrants(fake, 2115, data.ImmunityStun) {
		t.Fatalf("itemGrants should return true when status is in GrantsImmunity")
	}
	if !itemGrants(fake, 2115, data.ImmunitySleep) {
		t.Fatalf("itemGrants should return true for second status in list")
	}
}
