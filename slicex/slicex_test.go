package slicex_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/jun-develop/junkit/slicex"
)

func TestMap_Results(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want string
	}{
		{"maps each element in order", []int{1, 2, 3}, `["1","2","3"]`},
		{"nil input marshals as an empty list", nil, `[]`},
		{"empty input marshals as an empty list", []int{}, `[]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(slicex.Map(tt.in, strconv.Itoa))
			if err != nil || string(b) != tt.want {
				t.Fatalf("Map(%v) marshals to %s (err %v), want %s", tt.in, b, err, tt.want)
			}
		})
	}
}
