`mobl` - cli tool to generate a simple visualization of a go project call graph

## Example usage

Using graphviz for small projects:
```
go install github.com/worldsayshi/mobl@latest
git clone https://github.com/golang/example golang-example
mobl -png callgraph.png ./golang-example/
open callgraph.png
```

Using Gephi for large projects:
```
go install github.com/worldsayshi/mobl@latest
git clone https://github.com/junegunn/fzf
mobl -graphml callgraph.graphml ./fzf
# Then open callgraph.graphml in Gephi
```
