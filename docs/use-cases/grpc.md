# gRPC and other transports

A gateway or a BFF often parses the HTTP query and asks another service for the page over gRPC or a
queue. The package has no protobuf dependency: pass the request as JSON, and map the meta into
your own messages.

## Passing the request

`paginate.Request` is a plain struct with JSON tags. The whole request survives the trip, the order
of the `sortBy` keys included:

```go
// caller
payload, err := json.Marshal(req)
resp, err := client.ListOrders(ctx, &pb.ListOrdersRequest{Pagination: string(payload)})

// service
var req paginate.Request
if err := json.Unmarshal([]byte(in.GetPagination()), &req); err != nil {
	return nil, status.Error(codes.InvalidArgument, "pagination")
}
page, err := gormx.Paginate[Order](ctx, req, ordersPage, s.db.Model(&Order{}))
if errors.Is(err, paginate.ErrInvalidRequest) {
	return nil, status.Error(codes.InvalidArgument, err.Error())
}
```

The service checks the request against its own config, as for HTTP.

A request built from typed proto fields loses the order of the `sortBy` keys when the proto keeps
them in a map. Send the keys as a repeated field and set `SortKeys`, or the keys follow
`SortableColumns` order.

## Passing the meta back

Define the meta in your proto and copy the fields:

```proto
message PaginationMeta {
  uint64 items_per_page = 1;
  uint64 total_items = 2;
  uint64 current_page = 3;
  uint64 total_pages = 4;
}
```

```go
// service
meta := page.Meta
return &pb.ListOrdersResponse{
	Items: toProto(page.Data),
	Meta: &pb.PaginationMeta{
		ItemsPerPage: meta.ItemsPerPage,
		TotalItems:   meta.TotalItems,
		CurrentPage:  meta.CurrentPage,
		TotalPages:   meta.TotalPages,
	},
}, nil
```

The caller rebuilds the HTTP answer from the meta it got and the request it parsed:
`paginate.NewResponse(items, *req, resp.GetMeta().GetTotalItems())` gives the same meta, and the
links when the caller ran `paginate.Prepare` on the request with its own config.
