# OpenAPI

Declare the parameters and the meta once in `components` and reference them from every list
operation.

```yaml
components:
  parameters:
    Page:
      name: page
      in: query
      schema: { type: integer, minimum: 1, default: 1 }
    Limit:
      name: limit
      in: query
      description: Page size, capped by the endpoint (100 by default). -1 asks for every row where the endpoint allows it.
      schema: { type: integer, minimum: -1, default: 20 }
    SortBy:
      name: sortBy
      in: query
      description: "`field:asc` or `field:desc`, repeated; the first one is the main key."
      style: form
      explode: true
      schema: { type: array, items: { type: string, example: "createdAt:desc" } }
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

  schemas:
    Meta:
      type: object
      required: [itemsPerPage, totalItems, currentPage, totalPages, sortBy, filter, paginationMode]
      properties:
        itemsPerPage: { type: integer }
        totalItems: { type: integer }
        currentPage: { type: integer }
        totalPages: { type: integer }
        sortBy:
          type: object
          nullable: true
          additionalProperties: { type: string, enum: [asc, desc] }
        search: { type: string, nullable: true }
        searchBy: { type: array, nullable: true, items: { type: string } }
        filter:
          type: object
          nullable: true
          additionalProperties: { type: array, items: { type: string } }
        paginationMode: { type: string, enum: [default, count, disabled] }
```

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
          description: "`$eq` or `$in`: `filter.status=$in:paid,shipped`."
          style: form
          explode: true
          schema: { type: array, items: { type: string } }
        - name: filter.total
          in: query
          description: "`$btw`, `$gte`, `$lte`: `filter.total=$btw:100,500`."
          style: form
          explode: true
          schema: { type: array, items: { type: string } }
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                required: [data, meta]
                properties:
                  data: { type: array, items: { $ref: "#/components/schemas/Order" } }
                  meta: { $ref: "#/components/schemas/Meta" }
```

With `FormatNestJS` the meta and the links have nestjs-paginate's shape:

```yaml
    NestJSMeta:
      type: object
      required: [itemsPerPage, totalItems, currentPage, totalPages, sortBy]
      properties:
        itemsPerPage: { type: integer }
        totalItems: { type: integer }
        currentPage: { type: integer }
        totalPages: { type: integer }
        sortBy:
          type: array
          items: { type: array, minItems: 2, maxItems: 2, items: { type: string } }
          example: [["createdAt", "DESC"]]
        search: { type: string }
        searchBy: { type: array, items: { type: string } }
        filter:
          type: object
          additionalProperties:
            oneOf: [{ type: string }, { type: array, items: { type: string } }]
    Links:
      type: object
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
