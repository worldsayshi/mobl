`mobl` - cli tool to generate a simple visualization of a go project call graph

## Requirements

```bash
sudo apt-get update
sudo apt-get install graphviz
```

## Example usage

Using graphviz for small projects:
```
go install github.com/worldsayshi/mobl@latest
git clone https://github.com/golang/example golang-example
mobl -dotgraph callgraph.dot ./golang-example/
dot -Tpng callgraph.dot -o callgraph.png
open callgraph.png
```

Using Gephi for large projects:
```
go install github.com/worldsayshi/mobl@latest
git clone https://github.com/junegunn/fzf
mobl -graphml callgraph.graphml ./fzf
# Then open callgraph.graphml in Gephi
```
