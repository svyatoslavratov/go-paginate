# gRPC and other transports

A gateway or a BFF often parses the HTTP query and asks another service for the page over gRPC or a
queue. The package has no protobuf dependency: pass the request as JSON, and map the meta into
your own messages.

## Passing the request

`paginate.Request` is a plain struct with JSON tags. The whole request survives the trip: the sort
keys in their order, the filters, the cursor, and the path the links start with.

```go
// caller
payload, err := json.Marshal(req)
if err != nil {
	return err
}
resp, err := client.ListOrders(ctx, &pb.ListOrdersRequest{Pagination: string(payload)})
```

```go
// service
func (s *Server) ListOrders(ctx context.Context, in *pb.ListOrdersRequest) (*pb.ListOrdersResponse, error) {
	var req paginate.Request
	if err := json.Unmarshal([]byte(in.GetPagination()), &req); err != nil {
		return nil, status.Error(codes.InvalidArgument, "pagination: "+err.Error())
	}
	req.Mode = "" // what the client asked for stays in RequestedMode
	page, err := gormx.Paginate[Order](ctx, req, ordersEndpoint, s.db.Model(&Order{}))
	if errors.Is(err, paginate.ErrInvalidRequest) {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	return &pb.ListOrdersResponse{Items: toProto(page.Data), Meta: metaToProto(page.Meta)}, nil
}
```

`GET /orders?page=2&limit=10&sortBy=total:DESC&sortBy=createdAt:ASC&filter.status=$in:paid,shipped`
travels as:

```json
{"page":2,"limit":10,"sortBy":[["total","DESC"],["createdAt","ASC"]],"filter":{"status":["$in:paid,shipped"]},"mode":"default","requestedMode":"default","path":"/orders"}
```

The service checks the request against its own endpoint, as for HTTP, with one exception: a
`Mode` set in the request is applied as is, the way server code sets one for an
[export](exports-and-counts.md). A service that takes requests it did not build clears it, as
above. The client's wish stays in `RequestedMode`: `limit=0` still counts, and `limit=-1` still
waits for `AllowDisabledPagination`.

To pass the code and the field of a client's mistake on, take the `*paginate.Error` out with
`errors.As` and put them into the status details; see [errors](../errors.md).

## Typed requests

A request built from typed proto fields keeps the sort order in a repeated field:

```proto
message SortKey {
  string field = 1;
  string order = 2; // ASC or DESC
}
```

```go
req := paginate.Request{Page: in.GetPage(), Limit: in.GetLimit(), Search: in.GetSearch(), Cursor: in.GetCursor()}
for _, key := range in.GetSortBy() {
	req.SortBy = append(req.SortBy, paginate.Sort{Field: key.GetField(), Order: paginate.Order(key.GetOrder())})
}
```

`Prepare` takes the order in any case and answers anything but `ASC` and `DESC` with
`invalid_sort`. Set `RequestedMode` for the count only or every row, as the parsers do for
`limit=0` and `limit=-1`.

## Passing the meta back

Define the meta in your proto and copy the fields:

```proto
message PaginationMeta {
  uint64 items_per_page = 1;
  optional uint64 total_items = 2; // unset when not counted
  optional uint64 total_pages = 3;
  bool estimated = 4;
  uint64 current_page = 5;         // 0 in cursor pagination
  bool has_next_page = 6;
  bool has_previous_page = 7;
  string next_cursor = 8;
  string previous_cursor = 9;
  repeated SortKey sort_by = 10;   // the effective sort, the default included
}
```

```go
func metaToProto(meta paginate.Meta) *pb.PaginationMeta {
	out := &pb.PaginationMeta{
		ItemsPerPage:    meta.ItemsPerPage,
		Estimated:       meta.Count == paginate.CountEstimate,
		CurrentPage:     meta.CurrentPage,
		HasNextPage:     meta.HasNextPage,
		HasPreviousPage: meta.HasPreviousPage,
		NextCursor:      meta.NextCursor,
		PreviousCursor:  meta.PreviousCursor,
	}
	if meta.Count != paginate.CountNone {
		out.TotalItems, out.TotalPages = &meta.TotalItems, &meta.TotalPages
	}
	for _, key := range meta.SortBy {
		out.SortBy = append(out.SortBy, &pb.SortKey{Field: key.Field, Order: string(key.Order)})
	}
	return out
}
```

`Count` tells whether there are totals. Under `CountNone` and in cursor pagination they are zero,
which a plain `uint64` cannot tell from an empty result; hence `optional`. The cursors are opaque
strings: the caller sends one back in `Request.Cursor` with the same `sortBy`, since a cursor made
for another sort is a 400 (`invalid_cursor`). See [pages and counts](../pagination.md).

## Answering HTTP on the caller

The caller answers with its own types, filled from the message. A `paginate.Meta` filled in by
hand marshals in the default format; the nestjs view and the links come only from `NewResponse`,
on the service. A caller that answers in the nestjs format, or only forwards the answer, gets them
as JSON instead:

```go
// service
meta, err := json.Marshal(page.Meta) // in the endpoint's format
if err != nil {
	return nil, err
}
links, err := json.Marshal(page.Meta.Links()) // first, previous, current, next, last
if err != nil {
	return nil, err
}
```

The links start with `Request.Path`, the path the caller parsed, which travels with the request,
prefixed with the `LinksBaseURL` of the service's endpoint. `Meta.NestJS()` gives the nestjs view
as a struct; its totals are pointers, nil when not counted.
