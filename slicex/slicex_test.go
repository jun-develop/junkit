package slicex_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: slicex_test.go
// Purpose: Verify that Map converts every element in order and never returns a nil slice.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 3
//
// #  Test Case                    Scenario        Expected Result
// 1  TestMap_ConvertsEachElement  Three integers  Marshals to ["1","2","3"] (same order)
// 2  TestMap_ConvertsEachElement  Nil input       Marshals to [] (not null)
// 3  TestMap_ConvertsEachElement  Empty input     Marshals to []
//
// =============================================================================

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/jun-develop/junkit/slicex"
)

func TestMap_ConvertsEachElement(t *testing.T) {
	tests := []struct {
		name  string
		input []int
		want  string
	}{
		{"maps each element in order", []int{1, 2, 3}, `["1","2","3"]`},
		{"nil input marshals as an empty list", nil, `[]`},
		{"empty input marshals as an empty list", []int{}, `[]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(slicex.Map(tt.input, strconv.Itoa))

			if err != nil {
				t.Fatalf("json.Marshal(Map(%v)) error = %v", tt.input, err)
			}

			if string(got) != tt.want {
				t.Errorf("Map(%v) marshals to %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}
