# OpenAPI

Declare the parameters, the meta and the error once in `components` and reference them from every
list operation.

```yaml
components:
  parameters:
    Page:
      name: page
      in: query
      description: Page number, from 1. Endpoints with cursor pagination ignore it.
      schema: { type: integer, minimum: 1, default: 1 }
    Limit:
      name: limit
      in: query
      description: Page size, capped by the endpoint (100 by default). 0 asks for the count only, -1 for every row where the endpoint allows it.
      schema: { type: integer, minimum: -1, default: 20 }
    SortBy:
      name: sortBy
      in: query
      description: "`field:ASC` or `field:DESC`, repeated; the first one is the main key."
      style: form
      explode: true
      schema: { type: array, items: { type: string, example: "createdAt:DESC" } }
    Search:
      name: search
      in: query
      schema: { type: string, maxLength: 256 }
    SearchBy:
      name: searchBy
      in: query
      style: form
      explode: true
      schema: { type: array, items: { type: string } }
    PaginationMode:
      name: paginationMode
      in: query
      schema: { type: string, enum: [default, count, disabled], default: default }
    FilterExpression:
      name: filter
      in: query
      description: "AND/OR/NOT of `field=$operator:value` terms: `(status=$in:paid,shipped OR total=$gte:1000) AND NOT comment=$null`."
      schema: { type: string }
    Cursor:
      name: cursor
      in: query
      description: "The page of an endpoint with cursor pagination: `meta.nextCursor` or `meta.previousCursor` of the previous answer, sent with the same `sortBy`. None for the first page."
      schema: { type: string, maxLength: 4096 }

  schemas:
    SortKey:
      description: "[field, order], the order ASC or DESC."
      type: array
      minItems: 2
      maxItems: 2
      items: { type: string }
      example: ["createdAt", "DESC"]
    Meta:
      type: object
      required: [itemsPerPage, hasNextPage, hasPreviousPage, sortBy, search, searchBy, filter, paginationMode]
      properties:
        itemsPerPage: { type: integer, description: "The page size; for the count only and for every row, the number of items." }
        totalItems: { type: integer, description: "Missing when the endpoint does not count." }
        totalPages: { type: integer, description: "Missing when the endpoint does not count." }
        estimated: { type: boolean, description: "Present, and true, when totalItems is the planner's estimate." }
        currentPage: { type: integer, description: "Missing in cursor pagination." }
        hasNextPage: { type: boolean }
        hasPreviousPage: { type: boolean }
        sortBy: { type: array, items: { $ref: "#/components/schemas/SortKey" } }
        search: { type: string }
        searchBy: { type: array, items: { type: string } }
        filter:
          type: object
          additionalProperties: { type: array, items: { type: string } }
        paginationMode: { type: string, enum: [default, count, disabled] }
        cursor: { type: string, description: "The cursor of this page, in cursor pagination." }
        nextCursor: { type: string, description: "The cursor of the next page. With hasNextPage and no nextCursor, the next page is the first one." }
        previousCursor: { type: string, description: "The cursor of the previous page." }
    Error:
      type: object
      required: [code, message]
      properties:
        code:
          type: string
          enum: [invalid_page, invalid_sort, sort_not_allowed, search_not_allowed, search_too_long,
            unknown_field, filter_not_allowed, operator_not_allowed, invalid_value, too_many_values,
            invalid_expression, invalid_cursor]
        field: { type: string, description: "The API field at fault; missing when the error is about the request as a whole." }
        message: { type: string }
      example: { code: invalid_value, field: total, message: "invalid value \"lots\" for total: must be a number" }

  responses:
    BadRequest:
      description: The request does not fit the endpoint.
      content:
        application/json:
          schema: { $ref: "#/components/schemas/Error" }
```

This `Meta` is the shape of `FormatDefault`. `sortBy`, `searchBy` and `filter` are never `null`:
they are `[]` and `{}` when empty. The totals are there only when the endpoint counts, see
[pages and counts](../pagination.md#in-the-answer).

The error is the body of every 400, `*paginate.Error` encoded as is; `invalid_config` and
database errors are 500s, answered with a generic body. See [errors](../errors.md).

OpenAPI cannot describe a family of parameter names such as `filter.*`. Declare every filter of an
operation as its own parameter, with the operators it accepts:

```yaml
paths:
  /orders:
    get:
      operationId: listOrders
      parameters:
        - $ref: "#/components/parameters/Page"
        - $ref: "#/components/parameters/Limit"
        - $ref: "#/components/parameters/SortBy"
        - $ref: "#/components/parameters/Search"
        - name: filter.status
          in: query
          description: "`$eq` or `$in` of new, paid, shipped: `filter.status=$in:paid,shipped`."
          style: form
          explode: true
          schema: { type: array, items: { type: string } }
        - name: filter.total
          in: query
          description: "`$eq`, `$btw`, `$gte` or `$lte` of a number: `filter.total=$btw:100,500`."
          style: form
          explode: true
          schema: { type: array, items: { type: string } }
      responses:
        "200":
          description: A page of orders.
          content:
            application/json:
              schema:
                type: object
                required: [data, meta]
                properties:
                  data: { type: array, items: { $ref: "#/components/schemas/Order" } }
                  meta: { $ref: "#/components/schemas/Meta" }
        "400":
          $ref: "#/components/responses/BadRequest"
```

The endpoint knows its filters, so a generator or a test can keep these parameters in step with
it through `Endpoint.Fields()`:

```go
for _, field := range ordersEndpoint.Fields() {
	if field.Filter != nil {
		fmt.Printf("filter.%s: %s, operators %v\n", field.Name, field.Type, field.Filter)
	}
}
```

```text
filter.status: enum(new|paid|shipped), operators [$eq $in]
filter.total: number, operators [$eq $btw $gte $lte]
```

`$eq` is allowed on every filterable field, so `Fields` lists it whether the config did or not.

With `FormatNestJS` the meta and the links have nestjs-paginate's shape, and the answer has
`links` next to `data` and `meta`:

```yaml
    NestJSMeta:
      type: object
      required: [itemsPerPage, sortBy]
      properties:
        itemsPerPage: { type: integer, description: "The page size; in cursor pagination the rows of this page, for the count only and for every row the number of items." }
        totalItems: { type: integer, description: "Missing when the endpoint does not count." }
        currentPage: { type: integer, description: "Missing in cursor pagination." }
        totalPages: { type: integer, description: "Missing when the endpoint does not count." }
        sortBy: { type: array, items: { $ref: "#/components/schemas/SortKey" } }
        search: { type: string, description: "Only with a search." }
        searchBy: { type: array, items: { type: string }, description: "The fields the search covered, only with a search." }
        filter:
          type: object
          additionalProperties:
            oneOf: [{ type: string }, { type: array, items: { type: string } }]
        cursor: { type: string, description: "The cursor of this page, in cursor pagination." }
    Links:
      type: object
      description: A link is missing when there is no such page; last also needs an exact count. Cursor pagination has previous, current and next only.
      properties:
        first: { type: string }
        previous: { type: string }
        current: { type: string }
        next: { type: string }
        last: { type: string }
```

Generators (oapi-codegen, openapi-typescript) then give typed parameters on both sides. With
oapi-codegen strict handlers, parse pagination from the raw `*http.Request` with
`paginate.ParseRequest`; the generated parameter struct is only for documentation.
