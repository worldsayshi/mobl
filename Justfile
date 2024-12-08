
run-ratel:
  #!/bin/bash
  set -euo pipefail
  docker run --rm -d -p 8000:8000 dgraph/ratel:latest

run-dgraph:
  #!/bin/bash
  set -euo pipefail
  docker run --rm -d -p 8080:8080 -p 9080:9080 dgraph/standalone:latest

run-docker-compose:
  #!/bin/bash
  set -euo pipefail
  docker-compose up -d

run-code-analysis:
  #!/bin/bash
  set -euo pipefail
  go run main.go ../go-game/
