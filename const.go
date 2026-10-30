package paginate

// Defaults for the zero fields of Config.
const (
	// DefaultLimit is the page size of a request without limit.
	DefaultLimit uint64 = 20
	// DefaultMaxLimit caps the limit a client may ask for.
	DefaultMaxLimit uint64 = 100
	// DefaultMaxSearchLength caps the search term, in characters.
	DefaultMaxSearchLength uint64 = 256
	// DefaultMaxFilterConditions caps the number of filter.<field> values in one request.
	DefaultMaxFilterConditions uint64 = 100
	// DefaultMaxFilterValues caps the values of one $in, $contains or $btw list.
	DefaultMaxFilterValues uint64 = 1000
	// DefaultMaxFilterExpressionComplexity caps the nodes of a filter=<expression>, as in
	// nestjs-paginate.
	DefaultMaxFilterExpressionComplexity uint64 = 100
)
