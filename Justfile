

run-docker-compose:
  #!/bin/bash
  set -euo pipefail
  docker-compose up -d

run-code-analysis:
  #!/bin/bash
  set -euo pipefail
  go run main.go ../go-game/


delete-dgraph-data:
  #!/bin/bash
  set -euo pipefail
  # curl -X POST localhost:8080/alter -d '{"drop_all": true}'
  rm -f callgraph.db
