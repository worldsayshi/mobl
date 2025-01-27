This is an experiment in using AI to generate a program for analyzing the call graph of a go program and visualize it using graphviz. Also I wanted to store the graph in a db for some reason.

## TODO

- [X] Try using https://github.com/dpapathanasiou/simple-graph instead of dgraph!
  - I like the simple-graph philosophy but I don't like the way they built the example for go. Too much indirection and templating!
  - [ ] Identify the sql statements needed to reimplement the current dgraph stuff
  - [ ] Feed that stuff to Claude/Avante and ask it to use sqlc
  - [X] For now non-sqlc stuff was used
- [X] Output a dot file, have a live-reload server look at the dot file, render the dot graph in some web dot graphviz visualizer


# Ideas
-

## Dependencies

sudo apt-get update
sudo apt-get install graphviz


## Example usage

```
go install github.com/worldsayshi/mobl@latest
git clone https://github.com/golang/example golang-example
mobl -graphml callgraph.graphml ./golang-example/ragserver/
# Then open callgraph.graphml in Gephi
```

## Original prompt

As a reference:
```
This program creates a call graph from Go source code and stores it in Dgraph.
It analyzes Go source files using Tree-sitter for parsing, extracts function
definitions and their calls to other functions, and builds a directed graph
representation of function calls.

Key components:
- Tree-sitter parsing: Uses go-tree-sitter to parse Go source files and extract
  function declarations and function calls using AST queries
- Graph building: Creates a graph where nodes are functions and edges represent
  function calls between them
- Dgraph storage: Stores the call graph in Dgraph with a schema where functions
  are nodes with name and filePath properties, and calls are edges between nodes

Usage:
  go run main.go <source_directory>

The program will:
1. Recursively scan the provided directory for .go files
2. Parse each file and extract function definitions and calls
3. Build an in-memory representation of the call graph
4. Store the graph in Dgraph for further analysis

Schema in Dgraph:
- Function type with properties:
  - name: string @index(exact)
  - filePath: string
  - calls: [uid] @reverse

Example Dgraph query to explore the call graph:
{
  functions(func: type(Function)) {
    name
    filePath
    calls {
      name
    }
  }
}
```