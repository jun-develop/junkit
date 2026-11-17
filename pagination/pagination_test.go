package pagination_test

import (
	"math"
	"testing"

	"github.com/jun-develop/junkit/pagination"
)

// intPtr returns a pointer to v.
func intPtr(v int) *int { return &v }

func TestNewPagination(t *testing.T) {
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

			if got.Page != tt.want.Page ||
				got.PageSize != tt.want.PageSize ||
				got.TotalItems != tt.want.TotalItems ||
				got.TotalPages != tt.want.TotalPages ||
				got.HasNext != tt.want.HasNext ||
				got.HasPrev != tt.want.HasPrev {
				t.Fatalf("NewPagination(%d, %d, %d) = %+v, want %+v",
					tt.page, tt.pageSize, tt.totalItems, got, tt.want)
			}

			assertIntPtrEqual(t, "NextPage", got.NextPage, tt.want.NextPage)
			assertIntPtrEqual(t, "PrevPage", got.PrevPage, tt.want.PrevPage)
		})
	}
}

// assertIntPtrEqual fails the test if got and want differ in nil-ness or value.
func assertIntPtrEqual(t *testing.T, field string, got, want *int) {
	t.Helper()

	switch {
	case got == nil && want == nil:
		return
	case got == nil || want == nil:
		t.Fatalf("%s = %v, want %v", field, derefOrNil(got), derefOrNil(want))
	case *got != *want:
		t.Fatalf("%s = %d, want %d", field, *got, *want)
	}
}

// derefOrNil returns *p, or nil when p is nil.
func derefOrNil(p *int) any {
	if p == nil {
		return nil
	}

	return *p
}

func TestRequest_SetDefaults(t *testing.T) {
	tests := []struct {
		name             string
		req              pagination.Request
		defaultSortBy    string
		defaultSortOrder string
		wantPage         int
		wantPageSize     int
		wantSortBy       string
		wantSortOrder    string
	}{
		{
			name:             "empty request gets every default",
			req:              pagination.Request{},
			defaultSortBy:    "created_at",
			defaultSortOrder: "desc",
			wantPage:         1,
			wantPageSize:     10,
			wantSortBy:       "created_at",
			wantSortOrder:    "desc",
		},
		{
			name:             "already-set fields are left untouched",
			req:              pagination.Request{Page: 3, PageSize: 25, SortBy: "name", SortOrder: "asc"},
			defaultSortBy:    "created_at",
			defaultSortOrder: "desc",
			wantPage:         3,
			wantPageSize:     25,
			wantSortBy:       "name",
			wantSortOrder:    "asc",
		},
		{
			name:             "negative page and page_size fall back to defaults too",
			req:              pagination.Request{Page: -1, PageSize: -1},
			defaultSortBy:    "created_at",
			defaultSortOrder: "desc",
			wantPage:         1,
			wantPageSize:     10,
			wantSortBy:       "created_at",
			wantSortOrder:    "desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.req
			req.SetDefaults(tt.defaultSortBy, tt.defaultSortOrder)

			if req.Page != tt.wantPage || req.PageSize != tt.wantPageSize ||
				req.SortBy != tt.wantSortBy || req.SortOrder != tt.wantSortOrder {
				t.Fatalf("SetDefaults() = %+v, want page=%d page_size=%d sort_by=%q sort_order=%q",
					req, tt.wantPage, tt.wantPageSize, tt.wantSortBy, tt.wantSortOrder)
			}
		})
	}
}

func TestRequest_GetOffset(t *testing.T) {
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

func TestRequest_GetLimit(t *testing.T) {
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
