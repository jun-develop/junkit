package pagination_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: pagination_test.go
// Purpose: Verify page metadata calculation and request defaults, offset and limit clamping.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 29
//
// #   Test Case                                   Scenario                     Expected Result
// 1   TestNewPagination_ComputesPageMetadata      Middle page                  Has next and prev pages
// 2   TestNewPagination_ComputesPageMetadata      First page                   Has next page, no prev page
// 3   TestNewPagination_ComputesPageMetadata      Last page                    Has prev page, no next page
// 4   TestNewPagination_ComputesPageMetadata      No items                     One total page, no next or prev
// 5   TestNewPagination_ComputesPageMetadata      Page 0                       Page clamped to 1
// 6   TestNewPagination_ComputesPageMetadata      Negative page size           Page size clamped to 1
// 7   TestNewPagination_ComputesPageMetadata      Page far past the last       Page kept; prev page is the last page
// 8   TestNewPagination_ComputesPageMetadata      Page one past the last       Prev page is the last page
// 9   TestRequest_SetDefaults_FillsMissingFields  Empty request                Every field gets its default
// 10  TestRequest_SetDefaults_FillsMissingFields  All fields already set       Fields left unchanged
// 11  TestRequest_SetDefaults_FillsMissingFields  Negative page and page size  Page and page size get defaults
// 12  TestRequest_GetOffset_ReturnsClampedOffset  Page 1                       Offset 0
// 13  TestRequest_GetOffset_ReturnsClampedOffset  Page 2, size 10              Offset 10
// 14  TestRequest_GetOffset_ReturnsClampedOffset  Page 3, size 5               Offset 10
// 15  TestRequest_GetOffset_ReturnsClampedOffset  Offset beyond int32          Clamped to math.MaxInt32
// 16  TestRequest_GetOffset_ReturnsClampedOffset  Offset overflows int64       Clamped to math.MaxInt32
// 17  TestRequest_GetOffset_ReturnsClampedOffset  Last page that fits int32    Exact offset 2147483640
// 18  TestRequest_GetOffset_ReturnsClampedOffset  Page 0                       Offset 0
// 19  TestRequest_GetOffset_ReturnsClampedOffset  Negative page                Offset 0
// 20  TestRequest_GetOffset_ReturnsClampedOffset  Negative page size           Uses the clamped limit (1)
// 21  TestRequest_GetOffset_ReturnsClampedOffset  Page size 0                  Uses the clamped limit (1)
// 22  TestRequest_GetOffset_ReturnsClampedOffset  Page size 1000               Uses the clamped limit (100)
// 23  TestRequest_GetLimit_ClampsPageSize         Page size 25                 25
// 24  TestRequest_GetLimit_ClampsPageSize         Page size 1 (lower bound)    1
// 25  TestRequest_GetLimit_ClampsPageSize         Page size 100 (upper bound)  100
// 26  TestRequest_GetLimit_ClampsPageSize         Page size 0                  Clamped to 1
// 27  TestRequest_GetLimit_ClampsPageSize         Negative page size           Clamped to 1
// 28  TestRequest_GetLimit_ClampsPageSize         Page size 101                Clamped to 100
// 29  TestRequest_GetLimit_ClampsPageSize         Page size math.MaxInt        Clamped to 100
//
// =============================================================================

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/jun-develop/junkit/pagination"
)

// intPtr returns a pointer to v.
func intPtr(v int) *int { return &v }

// formatPagination prints p with NextPage and PrevPage dereferenced, so failures show values, not addresses.
func formatPagination(p pagination.Pagination) string {
	deref := func(v *int) any {
		if v == nil {
			return nil
		}

		return *v
	}

	return fmt.Sprintf("{Page:%d PageSize:%d TotalItems:%d TotalPages:%d HasNext:%v HasPrev:%v NextPage:%v PrevPage:%v}",
		p.Page, p.PageSize, p.TotalItems, p.TotalPages, p.HasNext, p.HasPrev, deref(p.NextPage), deref(p.PrevPage))
}

func TestNewPagination_ComputesPageMetadata(t *testing.T) {
	tests := []struct {
		name       string
		page       int
		pageSize   int
		totalItems int64
		want       pagination.Pagination
	}{
		{
			name:       "middle page has both next and prev",
			page:       2,
			pageSize:   10,
			totalItems: 35,
			want: pagination.Pagination{
				Page: 2, PageSize: 10, TotalItems: 35, TotalPages: 4,
				HasNext: true, HasPrev: true,
				NextPage: intPtr(3), PrevPage: intPtr(1),
			},
		},
		{
			name:       "first page has no prev",
			page:       1,
			pageSize:   10,
			totalItems: 35,
			want: pagination.Pagination{
				Page: 1, PageSize: 10, TotalItems: 35, TotalPages: 4,
				HasNext: true, HasPrev: false,
				NextPage: intPtr(2), PrevPage: nil,
			},
		},
		{
			name:       "last page has no next",
			page:       4,
			pageSize:   10,
			totalItems: 35,
			want: pagination.Pagination{
				Page: 4, PageSize: 10, TotalItems: 35, TotalPages: 4,
				HasNext: false, HasPrev: true,
				NextPage: nil, PrevPage: intPtr(3),
			},
		},
		{
			name:       "no items still reports one page",
			page:       1,
			pageSize:   10,
			totalItems: 0,
			want: pagination.Pagination{
				Page: 1, PageSize: 10, TotalItems: 0, TotalPages: 1,
				HasNext: false, HasPrev: false,
			},
		},
		{
			name:       "zero page is clamped to 1",
			page:       0,
			pageSize:   10,
			totalItems: 35,
			want: pagination.Pagination{
				Page: 1, PageSize: 10, TotalItems: 35, TotalPages: 4,
				HasNext: true, HasPrev: false,
				NextPage: intPtr(2), PrevPage: nil,
			},
		},
		{
			name:       "negative page size is clamped to 1",
			page:       1,
			pageSize:   -5,
			totalItems: 3,
			want: pagination.Pagination{
				Page: 1, PageSize: 1, TotalItems: 3, TotalPages: 3,
				HasNext: true, HasPrev: false,
				NextPage: intPtr(2), PrevPage: nil,
			},
		},
		{
			name:       "page past the last clamps prev to the last page",
			page:       50,
			pageSize:   10,
			totalItems: 25,
			want: pagination.Pagination{
				Page: 50, PageSize: 10, TotalItems: 25, TotalPages: 3,
				HasNext: false, HasPrev: true,
				NextPage: nil, PrevPage: intPtr(3),
			},
		},
		{
			name:       "page one past the last points prev at the last page",
			page:       4,
			pageSize:   10,
			totalItems: 25,
			want: pagination.Pagination{
				Page: 4, PageSize: 10, TotalItems: 25, TotalPages: 3,
				HasNext: false, HasPrev: true,
				NextPage: nil, PrevPage: intPtr(3),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pagination.NewPagination(tt.page, tt.pageSize, tt.totalItems)

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("NewPagination(%d, %d, %d) = %s, want %s",
					tt.page, tt.pageSize, tt.totalItems, formatPagination(got), formatPagination(tt.want))
			}
		})
	}
}

func TestRequest_SetDefaults_FillsMissingFields(t *testing.T) {
	tests := []struct {
		name string
		req  pagination.Request
		want pagination.Request
	}{
		{
			name: "empty request gets every default",
			req:  pagination.Request{},
			want: pagination.Request{Page: 1, PageSize: 10, SortBy: "created_at", SortOrder: "desc"},
		},
		{
			name: "already-set fields are left untouched",
			req:  pagination.Request{Page: 3, PageSize: 25, SortBy: "name", SortOrder: "asc"},
			want: pagination.Request{Page: 3, PageSize: 25, SortBy: "name", SortOrder: "asc"},
		},
		{
			name: "negative page and page_size fall back to defaults too",
			req:  pagination.Request{Page: -1, PageSize: -1},
			want: pagination.Request{Page: 1, PageSize: 10, SortBy: "created_at", SortOrder: "desc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.req

			req.SetDefaults("created_at", "desc")

			if req != tt.want {
				t.Fatalf("SetDefaults(created_at, desc) = %+v, want %+v", req, tt.want)
			}
		})
	}
}

func TestRequest_GetOffset_ReturnsClampedOffset(t *testing.T) {
	tests := []struct {
		name string
		req  pagination.Request
		want int32
	}{
		{name: "first page starts at offset 0", req: pagination.Request{Page: 1, PageSize: 10}, want: 0},
		{name: "second page offsets by one page size", req: pagination.Request{Page: 2, PageSize: 10}, want: 10},
		{name: "third page with a smaller page size", req: pagination.Request{Page: 3, PageSize: 5}, want: 10},
		{name: "page past int32 clamps instead of going negative", req: pagination.Request{Page: 3_000_000_000, PageSize: 10}, want: math.MaxInt32},
		{name: "page whose product overflows int64 clamps", req: pagination.Request{Page: math.MaxInt64, PageSize: 100}, want: math.MaxInt32},
		{name: "last page that still fits int32", req: pagination.Request{Page: 214_748_365, PageSize: 10}, want: 2_147_483_640},
		{name: "zero page is offset 0", req: pagination.Request{Page: 0, PageSize: 10}, want: 0},
		{name: "negative page is offset 0", req: pagination.Request{Page: -5, PageSize: 10}, want: 0},
		{name: "negative page size uses the clamped limit", req: pagination.Request{Page: 3, PageSize: -10}, want: 2},
		{name: "zero page size uses the clamped limit", req: pagination.Request{Page: 3, PageSize: 0}, want: 2},
		{name: "oversized page size uses the clamped limit", req: pagination.Request{Page: 2, PageSize: 1000}, want: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.GetOffset(); got != tt.want {
				t.Fatalf("GetOffset() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRequest_GetLimit_ClampsPageSize(t *testing.T) {
	tests := []struct {
		name     string
		pageSize int
		want     int32
	}{
		{name: "in range is kept", pageSize: 25, want: 25},
		{name: "lower bound is kept", pageSize: 1, want: 1},
		{name: "upper bound is kept", pageSize: 100, want: 100},
		{name: "zero clamps to 1", pageSize: 0, want: 1},
		{name: "negative clamps to 1", pageSize: -7, want: 1},
		{name: "above 100 clamps to 100", pageSize: 101, want: 100},
		{name: "huge clamps to 100", pageSize: math.MaxInt, want: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := pagination.Request{PageSize: tt.pageSize}
			if got := req.GetLimit(); got != tt.want {
				t.Fatalf("GetLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}
