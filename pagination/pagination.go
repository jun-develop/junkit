// Package pagination parses request pagination parameters and computes SQL offset/limit
// and response metadata.
package pagination

import "math"

// Pagination holds navigation metadata returned alongside a list response.
type Pagination struct {
	Page       int   `json:"page" example:"1"`
	PageSize   int   `json:"page_size" example:"10"`
	TotalItems int64 `json:"total_items" example:"100"`
	TotalPages int   `json:"total_pages" example:"10"`
	HasNext    bool  `json:"has_next" example:"true"`
	HasPrev    bool  `json:"has_prev" example:"false"`
	NextPage   *int  `json:"next_page"`
	PrevPage   *int  `json:"prev_page"`
}

// PageRequest is the page part of a list query, for lists that neither search nor sort.
type PageRequest struct {
	Page     int `form:"page" binding:"omitempty,min=1" example:"1"`
	PageSize int `form:"page_size" binding:"omitempty,min=1,max=100" example:"10"`
}

// Request represents standard list query parameters.
type Request struct {
	PageRequest
	Search    string `form:"search" binding:"omitempty" example:"John"`
	SortBy    string `form:"sort_by" binding:"omitempty" example:"created_at"`
	SortOrder string `form:"sort_order" binding:"omitempty,oneof=asc desc" example:"desc"`
}

// NewPagination computes Pagination metadata for a page of results. A page past the last one keeps
// its Page but gets PrevPage = TotalPages, so clients can step back to real data.
//
// Usage:
//
//	meta := pagination.NewPagination(req.Page, req.PageSize, totalItems)
func NewPagination(page, pageSize int, totalItems int64) Pagination {
	if pageSize <= 0 {
		pageSize = 1
	}

	if page <= 0 {
		page = 1
	}

	totalPages := int(math.Ceil(float64(totalItems) / float64(pageSize)))
	if totalPages == 0 {
		totalPages = 1
	}

	hasNext := page < totalPages
	hasPrev := page > 1

	var nextPage, prevPage *int
	if hasNext {
		next := page + 1
		nextPage = &next
	}

	if hasPrev {
		prev := min(page-1, totalPages)
		prevPage = &prev
	}

	return Pagination{
		Page:       page,
		PageSize:   pageSize,
		TotalItems: totalItems,
		TotalPages: totalPages,
		HasNext:    hasNext,
		HasPrev:    hasPrev,
		NextPage:   nextPage,
		PrevPage:   prevPage,
	}
}

// SetDefaults fills default values for missing request pagination fields.
//
// Usage:
//
//	var req pagination.Request
//	_ = ctx.ShouldBindQuery(&req)
//	req.SetDefaults("created_at", "desc")
func (r *Request) SetDefaults(defaultSortBy, defaultSortOrder string) {
	r.PageRequest.SetDefaults()

	if r.SortBy == "" {
		r.SortBy = defaultSortBy
	}

	if r.SortOrder == "" {
		r.SortOrder = defaultSortOrder
	}
}

// SetDefaults fills a missing or invalid page (1) and page size (10).
//
// Usage:
//
//	var req pagination.PageRequest
//	_ = ctx.ShouldBindQuery(&req)
//	req.SetDefaults()
func (r *PageRequest) SetDefaults() {
	if r.Page <= 0 {
		r.Page = 1
	}

	if r.PageSize <= 0 {
		r.PageSize = 10
	}
}

// GetOffset returns the SQL OFFSET for the current page, never negative, using GetLimit as the
// page size so a manually built Request cannot skip validation.
func (r *PageRequest) GetOffset() int32 {
	if r.Page <= 1 {
		return 0
	}

	limit := int(r.GetLimit())

	// Clamped: an absurd page must give an empty page, not an overflowed (negative) OFFSET.
	if r.Page-1 > math.MaxInt32/limit {
		return math.MaxInt32
	}

	return int32((r.Page - 1) * limit)
}

// GetLimit returns the SQL LIMIT for the current page, clamped to [1, 100].
func (r *PageRequest) GetLimit() int32 {
	return int32(min(max(r.PageSize, 1), 100))
}
