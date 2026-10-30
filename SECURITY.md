# Security policy

## Supported versions

Fixes go into the latest minor release. Until v1, upgrade to the latest `v0.x.y` to get them.

## Reporting a vulnerability

Please do not open a public issue. Report privately through GitHub:
[Security → Report a vulnerability](https://github.com/svyatoslavratov/go-paginate/security/advisories/new).

Include the version, the query string and the config that show the problem, and what an attacker
gains. You will get an answer within 3 working days. A confirmed vulnerability gets a fix and a
GitHub security advisory; you are credited unless you ask not to be.

## Scope

In scope: anything a client can do through query parameters that the config does not allow.

- SQL injection through any parameter.
- Reaching fields, columns or operators the config does not list.
- Reading rows the base query excludes.
- Requests that cost the database far more than their caps suggest (denial of service).
- Error messages that leak data or schema details beyond what [docs/security.md](docs/security.md)
  describes.

Out of scope: configs that expose what they should not (sortable or filterable secret columns,
`AllowDisabledPagination` on a large public table). [docs/security.md](docs/security.md) explains
the settings and the checklist for an endpoint.
