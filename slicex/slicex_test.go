package slicex_test

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"

	"github.com/jun-develop/junkit/slicex"
)

func TestMap(t *testing.T) {
	if got := slicex.Map([]int{1, 2, 3}, strconv.Itoa); !slices.Equal(got, []string{"1", "2", "3"}) {
		t.Errorf("Map = %v, want [1 2 3]", got)
	}

	var none []int
	if b, _ := json.Marshal(slicex.Map(none, strconv.Itoa)); string(b) != "[]" {
		t.Errorf("Map(nil) marshals to %s, want []", b)
	}
}
