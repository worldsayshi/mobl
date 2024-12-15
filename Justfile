

run-docker-compose:
  #!/bin/bash
  set -euo pipefail
  docker-compose up -d

run-code-analysis:
  #!/bin/bash
  set -euo pipefail
  go run main.go ../go-game/


delete-output:
  #!/bin/bash
  set -euo pipefail
  # curl -X POST localhost:8080/alter -d '{"drop_all": true}'
  rm -f callgraph.*

render-dotfile:
  #!/bin/bash
  set -euo pipefail
  dot -Tpng callgraph.dot -o callgraph.png
