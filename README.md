## TODO

- [X] Try using https://github.com/dpapathanasiou/simple-graph instead of dgraph!
  - I like the simple-graph philosophy but I don't like the way they built the example for go. Too much indirection and templating!
  - [ ] Identify the sql statements needed to reimplement the current dgraph stuff
  - [ ] Feed that stuff to Claude/Avante and ask it to use sqlc
  - [X] For now non-sqlc stuff was used
- [X] Output a dot file, have a live-reload server look at the dot file, render the dot graph in some web dot graphviz visualizer 

## Dependencies

sudo apt-get update
sudo apt-get install graphviz
