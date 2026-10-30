package paginate

// Response is the {data, meta} answer of a list endpoint; with FormatNestJS its JSON adds links.
type Response[T any] struct {
	Data []T  `json:"data"`
	Meta Meta `json:"meta"`
}

// Meta describes the page and echoes the request as the client sent it (API field names), so a
// client can render its controls from the answer. Copy it whole into other answers: it carries
// the format, the nestjs-paginate view and the links.
type Meta struct {
	ItemsPerPage   uint64      `json:"itemsPerPage"`
	TotalItems     uint64      `json:"totalItems"`
	CurrentPage    uint64      `json:"currentPage"`
	TotalPages     uint64      `json:"totalPages"`
	SortBy         SortBy      `json:"sortBy"`
	Search         string      `json:"search"`
	SearchBy       []string    `json:"searchBy"`
	Filter         FilterQuery `json:"filter"`
	PaginationMode Mode        `json:"paginationMode"`

	format Format
	nest   *NestJSMeta
	links  *Links
}

// NewResponse builds the answer for a page of data out of totalItems matching rows. Data is never
// null in JSON. CurrentPage is the requested page within [1, max(TotalPages, 1)]. Without
// pagination everything is one page. A request that went through Prepare also gets the
// nestjs-paginate view and the links.
func NewResponse[T any](data []T, req Request, totalItems uint64) *Response[T] {
	if data == nil {
		data = []T{}
	}
	page := max(req.Page, 1)
	itemsPerPage := req.Limit
	totalPages := uint64(1)
	if req.Mode == ModeDisabled || req.Limit == 0 {
		itemsPerPage = totalItems
	} else {
		totalPages = totalItems / req.Limit
		if totalItems%req.Limit != 0 {
			totalPages++
		}
	}
	mode := req.Mode
	if mode == "" {
		mode = ModeDefault
	}
	searchBy := req.SearchBy
	if searchBy == nil {
		searchBy = []string{}
	}
	meta := Meta{
		ItemsPerPage:   itemsPerPage,
		TotalItems:     totalItems,
		CurrentPage:    min(page, max(totalPages, 1)),
		TotalPages:     totalPages,
		SortBy:         nonNilSortBy(req.SortBy),
		Search:         req.Search,
		SearchBy:       searchBy,
		Filter:         nonNilFilter(req.Filter),
		PaginationMode: mode,
	}
	if req.view != nil {
		presentResponse(&meta, &req, len(data), totalItems)
	}
	return &Response[T]{Data: data, Meta: meta}
}

func nonNilSortBy(sortBy SortBy) SortBy {
	if sortBy == nil {
		return SortBy{}
	}
	return sortBy
}

func nonNilFilter(filter FilterQuery) FilterQuery {
	if filter == nil {
		return FilterQuery{}
	}
	return filter
}
