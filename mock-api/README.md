# mock-api

Standalone mock API server used for local testing of skyline-mcp-api-bridge.

## What's included
- OpenAPI (pets)
- Swagger 2.0 (dinosaurs)
- WSDL / SOAP (plants)
- GraphQL (cars)
- OData v4 (movies)
- gRPC (clothes)

## Run

```bash
go run .
```

Server listens on `http://localhost:9999`.

### Spec endpoints

- OpenAPI spec: `http://localhost:9999/openapi/openapi.json`
- Swagger 2.0 spec: `http://localhost:9999/swagger/swagger.json`
- WSDL spec: `http://localhost:9999/wdsl/wsdl`
- GraphQL schema (SDL): `http://localhost:9999/graphql/schema`
- GraphQL endpoint: `http://localhost:9999/graphql` (POST)
- OData metadata: `http://localhost:9999/odata/$metadata`
- OData service root: `http://localhost:9999/odata/`

### REST base paths

- OpenAPI pets: `http://localhost:9999/openapi/pets`
- Swagger dinosaurs: `http://localhost:9999/swagger/dinosaurs`
- SOAP endpoint: `http://localhost:9999/wdsl/soap`

### OData (movies)

```
GET    /odata/Movies                  List movies
GET    /odata/Movies(1)               Get movie by ID
POST   /odata/Movies                  Create movie
PUT    /odata/Movies(1)               Full update
PATCH  /odata/Movies(1)               Partial update
DELETE /odata/Movies(1)               Delete movie
```

Supported OData query options:

- `$top=5` — limit results
- `$skip=2` — offset results
- `$orderby=Year desc` — sort by field
- `$filter=Year gt 2000` — filter (eq, ne, gt, lt, ge, le, contains)
- `$select=Title,Year` — select specific fields
- `$count=true` — include total count

Example: `GET /odata/Movies?$filter=contains(Genre,'Sci')&$orderby=Rating desc&$top=3`

Seed data: 8 movies (The Matrix, Inception, The Godfather, Pulp Fiction, Interstellar, The Dark Knight, Forrest Gump, Fight Club).

## gRPC

Clothes mocks listen on ports `50051–50054` (hats, shoes, pants, shirts).
