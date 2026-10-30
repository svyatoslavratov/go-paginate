# go-paginate

[![CI](https://github.com/svyatoslavratov/go-paginate/actions/workflows/ci.yml/badge.svg)](https://github.com/svyatoslavratov/go-paginate/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/svyatoslavratov/go-paginate.svg)](https://pkg.go.dev/github.com/svyatoslavratov/go-paginate)
[![Go Report Card](https://goreportcard.com/badge/github.com/svyatoslavratov/go-paginate)](https://goreportcard.com/report/github.com/svyatoslavratov/go-paginate)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/svyatoslavratov/go-paginate/badge)](https://scorecard.dev/viewer/?uri=github.com/svyatoslavratov/go-paginate)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[English version](README.md)

Пагинация, сортировка, поиск и фильтры для списковых ручек на Go по мотивам
[nestjs-paginate](https://github.com/ppetzold/nestjs-paginate). Язык запросов тот же: `page`,
`limit`, `sortBy`, `search`, `searchBy`, `filter.<поле>=$оператор:значение` и
`filter=<выражение>`. Ответ — `{data, meta}`, по выбору с `meta` и `links` как у nestjs-paginate.
Фронтенд, написанный под nestjs-paginate, работает с бэкендом на Go без изменений, с оговорками из
[сравнения](docs/nestjs-compatibility.md) с nestjs-paginate 16.0.1.

- **Два исполнителя, один SQL.** `gormx.Paginate` работает на GORM, `squirrelx.Paginate` — на squirrel и pgx.
  Условия у обоих собирает один и тот же код, поэтому ведут они себя одинаково.
- **Выражения фильтров.** `filter=(status=$in:paid,shipped OR total=$gte:1000) AND NOT comment=$null` —
  грамматика и ограничения как в nestjs-paginate 15+.
- **Ответ как у nestjs-paginate.** `FormatNestJS` даёт `meta.sortBy` парами, `meta.filter`
  строками и ссылки на соседние страницы.
- **Ядро без зависимостей.** Корневой пакет разбирает запросы net/http, проверяет их и собирает
  SQL-условия; исполнители для GORM и squirrel + pgx и парсер fasthttp лежат в своих пакетах, и
  в сборку попадает только то, что вы используете.
- **Белые списки.** Клиенту доступны только поля, колонки и операторы из конфига. Каждое значение
  передаётся параметром запроса.
- **Стабильные страницы.** Каждый `ORDER BY` заканчивается уникальной колонкой-тайбрейком, и строки
  не перескакивают между страницами.
- **Билдер для GORM.** Имена колонок он берёт из модели, имена полей API — из DTO. Переименованное
  поле ломает сборку, а не запрос.

## Установка

```sh
go get github.com/svyatoslavratov/go-paginate
```

```go
import (
	"github.com/svyatoslavratov/go-paginate"
	"github.com/svyatoslavratov/go-paginate/gormx"     // GORM
	"github.com/svyatoslavratov/go-paginate/squirrelx" // squirrel + pgx
	"github.com/svyatoslavratov/go-paginate/fasthttpx" // fasthttp
)
```

| Пакет | Импорт | Назначение |
|---|---|---|
| `paginate` | `github.com/svyatoslavratov/go-paginate` | Разбор (net/http), проверка и SQL запроса, сборка ответа. Без зависимостей. |
| `gormx` | `github.com/svyatoslavratov/go-paginate/gormx` | Исполнение на GORM, конфиги из моделей. |
| `squirrelx` | `github.com/svyatoslavratov/go-paginate/squirrelx` | Исполнение на select из squirrel и pgx. |
| `fasthttpx` | `github.com/svyatoslavratov/go-paginate/fasthttpx` | Разбор запросов fasthttp. |

Нужны Go 1.26 или новее и Postgres (SQL использует `ILIKE`, `@>` и `CAST`). Свой исполнитель
(database/sql, sqlx, bun) вызывает `paginate.Prepare` так же, как встроенные.

## Быстрый старт

```go
var usersPage = func() paginate.Config {
	cfg := paginate.NewConfig("users",
		[]string{"users.created_at", "users.name"}, // сортируемые колонки
		map[string]string{                          // поле API -> колонка
			"createdAt": "users.created_at",
			"name":      "users.name",
			"email":     "users.email",
			"status":    "users.status",
		})
	cfg.SearchableColumns = []string{"users.name", "users.email"}
	cfg.FilterableColumns = paginate.FilterableColumns{
		"users.status":     {paginate.OpIn}, // $eq разрешён всегда
		"users.created_at": {paginate.OpBtw},
	}
	cfg.DefaultSortBy = paginate.SortBy{"users.created_at": paginate.Desc}
	cfg.TieBreakColumn = "users.id"
	return *cfg
}()

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	req, err := paginate.ParseRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	page, err := gormx.Paginate[User](r.Context(), *req, usersPage, h.db.Model(&User{}))
	if errors.Is(err, paginate.ErrInvalidRequest) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
```

```http
GET /users?page=2&limit=10&sortBy=name:asc&search=ann&filter.status=$in:active,invited&filter.createdAt=$btw:2024-01-01,2024-12-31
```

```json
{
  "data": [{ "id": 11, "name": "Ann", "email": "ann@example.com", "status": "active" }],
  "meta": {
    "itemsPerPage": 10,
    "totalItems": 11,
    "currentPage": 2,
    "totalPages": 2,
    "sortBy": { "name": "asc" },
    "search": "ann",
    "searchBy": [],
    "filter": { "createdAt": ["$btw:2024-01-01,2024-12-31"], "status": ["$in:active,invited"] },
    "paginationMode": "default"
  }
}
```

С `cfg.Format = paginate.FormatNestJS` тот же запрос отвечает как nestjs-paginate:

```json
{
  "data": [{ "id": 11, "name": "Ann", "email": "ann@example.com", "status": "active" }],
  "meta": {
    "itemsPerPage": 10,
    "totalItems": 11,
    "currentPage": 2,
    "totalPages": 2,
    "sortBy": [["name", "ASC"]],
    "search": "ann",
    "searchBy": ["name", "email"],
    "filter": { "createdAt": "$btw:2024-01-01,2024-12-31", "status": "$in:active,invited" }
  },
  "links": {
    "first": "/users?page=1&limit=10&sortBy=name:ASC&search=ann&filter.createdAt=$btw:2024-01-01,2024-12-31&filter.status=$in:active,invited",
    "previous": "/users?page=1&limit=10&sortBy=name:ASC&search=ann&filter.createdAt=$btw:2024-01-01,2024-12-31&filter.status=$in:active,invited",
    "current": "/users?page=2&limit=10&sortBy=name:ASC&search=ann&filter.createdAt=$btw:2024-01-01,2024-12-31&filter.status=$in:active,invited"
  }
}
```

## Параметры запроса

| Параметр | Пример | Смысл |
|---|---|---|
| `page` | `page=2` | Номер страницы, с 1. |
| `limit` | `limit=50` | Размер страницы, не больше `MaxLimit` (по умолчанию 100). `limit=0` — только количество, `limit=-1` — все строки (если разрешено). |
| `sortBy` | `sortBy=pinned:desc&sortBy=name:asc` | Повторяемый `поле:asc\|desc`; порядок параметров задаёт приоритет. |
| `search` | `search=ann` | Поиск подстроки без учёта регистра по `SearchableColumns`. |
| `searchBy` | `searchBy=email` | Сужает поиск до части полей. |
| `filter.<поле>` | `filter.age=$gte:18` | Фильтр; повторите параметр, чтобы задать несколько условий на одно поле. |
| `filter` | `filter=age=$gte:18 OR status=$eq:vip` | Выражение: AND, OR, NOT и скобки над условиями `поле=значение`. |
| `paginationMode` | `paginationMode=count` | `default`, `count` (только количество) или `disabled` (все строки, если конфиг разрешает). |
| `withDeleted` | `withDeleted=true` | Вместе с мягко удалёнными строками (GORM, если конфиг разрешает). |

## Фильтры

Значение фильтра: `[$or:|$and:][$not:][$оператор:]значение`.

| Оператор | Пример | SQL |
|---|---|---|
| `$eq` (по умолчанию) | `filter.name=Milo` | `name = 'Milo'` |
| `$not` | `filter.name=$not:Milo` | `NOT (name = 'Milo')` |
| `$gt`, `$gte`, `$lt`, `$lte` | `filter.age=$gte:18` | `age >= 18` |
| `$in` | `filter.id=$in:1,2,3` | `id IN (1, 2, 3)` |
| `$btw` | `filter.age=$btw:4,6` | `age >= 4 AND age <= 6` |
| `$null` | `filter.seenAt=$null` | `seen_at IS NULL` |
| `$ilike` | `filter.title=$ilike:red` | `title ILIKE '%red%'` |
| `$sw` | `filter.title=$sw:red` | `title ILIKE 'red%'` |
| `$contains` | `filter.roles=$contains:admin,editor` | `roles @> '{admin,editor}'` (колонка-массив содержит все значения) |

Несколько значений одного поля образуют цепочку в порядке запроса:
`filter.id=$gt:3&filter.id=$lt:5&filter.id=$or:$eq:7` — это `(id > 3 AND id < 5 OR id = 7)`.
Разные поля соединяются через AND, но поля, у которых первое значение начинается с `$or`, собираются
в одну OR-группу. Любую другую логику задаёт выражение:

```http
GET /cats?filter=(color=$eq:black OR color=$eq:white) AND NOT name=$eq:Leche
```

Значение сравнивается по типу колонки: `$eq:10.5` находит `10.50`, а значение, не подходящее под тип,
даёт 400. `%` и `_` в `$ilike`, `$sw` и `search` ищутся буквально. Подробности — в
[docs/filters.md](docs/filters.md).

## Конфигурация

| Поле | По умолчанию | Смысл |
|---|---|---|
| `TableName` | обязательно | Таблица; билдер добавляет её к колонкам. |
| `SortableColumns` | обязательно | Колонки для сортировки; первая — сортировка на крайний случай. |
| `FieldColumns` | | Поле API → колонка для `sortBy`, `searchBy` и `filter`. |
| `DefaultSortBy`, `DefaultSortKeys` | первая сортируемая по возрастанию | Сортировка запроса без `sortBy`. |
| `TieBreakColumn` | у билдера — первичный ключ | Уникальная колонка в конце каждого `ORDER BY`. |
| `SearchableColumns`, `MultiWordSearch` | | Колонки поиска; с `MultiWordSearch` должно совпасть каждое слово. |
| `FilterableColumns` | | Колонка → разрешённые операторы; `{}` — только `$eq`, `AllOperators` — все. |
| `DefaultLimit`, `MaxLimit` | 20, 100 | Размер страницы. |
| `MaxSearchLength`, `MaxFilterConditions`, `MaxFilterValues`, `MaxFilterExpressionComplexity` | 256, 100, 1000, 100 | Ограничения запроса. |
| `StrictValidate` | `true` в `NewConfig` | Неизвестные поля дают 400, а не отбрасываются. |
| `AllowDisabledPagination` | `false` | Клиент может попросить все строки. |
| `AllowWithDeletedInQuery` | `false` | Клиент может попросить мягко удалённые строки. |
| `CompareAsText` | `false` | Сравнивать `$eq`/`$in` как текст — поведение до типизированного сравнения. |
| `Format` | свой формат | `FormatNestJS` — `meta` и `links` как у nestjs-paginate. |
| `LinksBaseURL` | относительные ссылки | Префикс ссылок: origin или путь. |

Вызывайте `config.Validate()` в юнит-тесте: он находит колонки, до которых не доходит ни одно поле
API, неизвестные операторы и поля, которые не смог разобрать билдер. См.
[docs/configuration.md](docs/configuration.md) (документация — на английском).

## Ошибки

- `ErrInvalidRequest` — ошибка клиента: неизвестное поле, запрещённый оператор, кривое,
  слишком длинное или не подходящее по типу значение, кривое выражение. Ответ 400; сообщение можно
  показать клиенту.
- `ErrInvalidConfig` — ошибка сервера. Ответ 500.

## nestjs-paginate

Сравнение с nestjs-paginate **16.0.1**:

| | nestjs-paginate | go-paginate |
|---|---|---|
| Параметры запроса, операторы, `$not` | ✅ | ✅ |
| Выражения `filter=` | ✅ | ✅ |
| `$and`/`$or` внутри `filter.<поле>` | убраны в 15.0 | оставлены (правила 14.x) |
| `limit=0` — только количество, `limit=-1` — все строки | ✅ | ✅ (`limit=-1` за опцией) |
| `meta` с фактической сортировкой, `links` | ✅ | ✅ за `FormatNestJS` |
| Сравнение по типу, `$contains` как «массив содержит» | ✅ | ✅ |
| `multiWordSearch`, `withDeleted`, `ignoreSearchByInQueryParam` | ✅ | ✅ |
| Маски LIKE в поиске | маски | буквально |
| Неизвестные поля | отбрасываются | 400 со `StrictValidate` |
| `select`, курсорная пагинация, связи, пути JSONB | ✅ | ❌ |
| Тайбрейк, ограничения, `Validate`, билдер GORM | ❌ | ✅ |

Полное [сравнение](docs/nestjs-compatibility.md) и
[руководство по миграции](docs/migrating-from-nestjs.md), где каждая фича показана на TypeScript и на Go.

## Документация

- [Параметры запроса](docs/query-syntax.md), [фильтры](docs/filters.md), [сортировка и тайбрейк](docs/sorting.md)
- [Конфигурация](docs/configuration.md), [безопасность](docs/security.md)
- [Сравнение с nestjs-paginate](docs/nestjs-compatibility.md), [миграция с nestjs-paginate](docs/migrating-from-nestjs.md)
- Сценарии: [net/http + GORM](docs/use-cases/chi-gorm.md), [билдер GORM](docs/use-cases/gorm-builder.md),
  [squirrel + pgx и fasthttp](docs/use-cases/squirrel-pgx.md), [JOIN, алиасы, GROUP BY](docs/use-cases/joins-and-aliases.md),
  [выгрузки и подсчёт](docs/use-cases/exports-and-counts.md), [gRPC](docs/use-cases/grpc.md),
  [OpenAPI](docs/use-cases/openapi.md), [свой исполнитель](docs/use-cases/custom-executor.md), [тест конфига](docs/use-cases/testing.md)
- [Changelog](CHANGELOG.md)

## Участие в разработке

Issues и pull requests приветствуются, см. [CONTRIBUTING.md](CONTRIBUTING.md). Тесты: `make test`,
с одноразовым Postgres: `make test-pg`. Об уязвимостях сообщайте закрыто, как описано в
[SECURITY.md](SECURITY.md).

## Лицензия

[MIT](LICENSE).
