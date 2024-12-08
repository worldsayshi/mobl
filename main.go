// This program creates a call graph from Go source code and stores it in Dgraph.
// It analyzes Go source files using Tree-sitter for parsing, extracts function
// definitions and their calls to other functions, and builds a directed graph
// representation of function calls.
//
// Key components:
// - Tree-sitter parsing: Uses go-tree-sitter to parse Go source files and extract
//   function declarations and function calls using AST queries
// - Graph building: Creates a graph where nodes are functions and edges represent
//   function calls between them
// - Dgraph storage: Stores the call graph in Dgraph with a schema where functions
//   are nodes with name and filePath properties, and calls are edges between nodes
//
// Usage:
//   go run main.go <source_directory>
//
// The program will:
// 1. Recursively scan the provided directory for .go files
// 2. Parse each file and extract function definitions and calls
// 3. Build an in-memory representation of the call graph
// 4. Store the graph in Dgraph for further analysis
//
// Schema in Dgraph:
// - Function type with properties:
//   - name: string @index(exact)
//   - filePath: string
//   - calls: [uid] @reverse
//
// Example Dgraph query to explore the call graph:
// {
//   functions(func: type(Function)) {
//     name
//     filePath
//     calls {
//       name
//     }
//   }
// }


package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
  "strings"

	"github.com/dgraph-io/dgo/v2"
	"github.com/dgraph-io/dgo/v2/protos/api"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"google.golang.org/grpc"
)

type Function struct {
	Name     string   `json:"name"`
	FilePath string   `json:"filePath"`
	Calls    []string `json:"calls"`
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: program <source_directory>")
	}
	sourceDir := os.Args[1]

	log.Printf("Analyzing source directory: %s", sourceDir)

	// Initialize Tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	// Map to store all functions and their calls
	functionMap := make(map[string]*Function)

	// Walk through all .go files in the directory
	fileCount := 0
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".go" {
			fileCount++
			log.Printf("Processing file: %s", path)
			err := processFile(path, parser, functionMap)
			if err != nil {
				log.Printf("Error processing %s: %v", path, err)
			}
		}
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Processed %d files", fileCount)
	log.Printf("Found %d functions", len(functionMap))

	// Store in Dgraph
	log.Println("Storing data in Dgraph...")
	err = storeToDgraph(functionMap)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Data storage complete")
}

func processFile(filePath string, parser *sitter.Parser, functionMap map[string]*Function) error {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("error reading file: %v", err)
	}

	// Parse the file
	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return fmt.Errorf("error parsing file: %v", err)
	}
	defer tree.Close()

	// Query to find function declarations
	query, err := sitter.NewQuery([]byte(`
		(function_declaration
			name: (identifier) @func.name
			body: (block) @func.body)
	`), golang.GetLanguage())
	if err != nil {
		return fmt.Errorf("error creating query: %v", err)
	}
	defer query.Close()

	// Execute the query
	cursor := sitter.NewQueryCursor()
	cursor.Exec(query, tree.RootNode())

	// Process each function
	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		// Get function name
		for _, capture := range match.Captures {
			if capture.Index == 0 { // func.name capture
				funcName := content[capture.Node.StartByte():capture.Node.EndByte()]
				
				// Create function entry
				function := &Function{
					Name:     string(funcName),
					FilePath: filePath,
					Calls:    []string{},
				}

				// Find function calls in the body
				bodyNode := match.Captures[1].Node // func.body capture
				callQuery, err := sitter.NewQuery([]byte(`
					(call_expression
						function: (identifier) @call.name)
				`), golang.GetLanguage())
				if err != nil {
					return fmt.Errorf("error creating call query: %v", err)
				}
				defer callQuery.Close()

				callCursor := sitter.NewQueryCursor()
				callCursor.Exec(callQuery, bodyNode)

				for {
					callMatch, ok := callCursor.NextMatch()
					if !ok {
						break
					}

					for _, callCapture := range callMatch.Captures {
						calledFunc := content[callCapture.Node.StartByte():callCapture.Node.EndByte()]
						function.Calls = append(function.Calls, string(calledFunc))
					}
				}

				functionMap[string(funcName)] = function
			}
		}
	}

	return nil
}

func storeToDgraph(functionMap map[string]*Function) error {
	// Connect to Dgraph
	log.Println("Connecting to Dgraph...")
	conn, err := grpc.Dial("localhost:9080", grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("error connecting to Dgraph: %v", err)
	}
	defer conn.Close()

	client := dgo.NewDgraphClient(api.NewDgraphClient(conn))

	// Set schema
	log.Println("Setting Dgraph schema...")
	op := &api.Operation{
		Schema: `
			name: string @index(exact) .
			filePath: string .
			calls: [uid] @reverse .
			type Function {
				name
				filePath
				calls
			}
		`,
	}

	err = client.Alter(context.Background(), op)
	if err != nil {
		return fmt.Errorf("error setting schema: %v", err)
	}

	// Create a map of function name to uid
	uidMap := make(map[string]string)

	// First pass: Create all function nodes
	log.Println("Creating function nodes...")
	for funcName, function := range functionMap {
		mutation := &api.Mutation{
			SetNquads: []byte(fmt.Sprintf(`
				_:%s <name> %q .
				_:%s <filePath> %q .
				_:%s <dgraph.type> "Function" .
			`, funcName, function.Name, funcName, function.FilePath, funcName)),
		}

		resp, err := client.NewTxn().Mutate(context.Background(), mutation)
		if err != nil {
			return fmt.Errorf("error creating function node: %v", err)
		}

		uidMap[funcName] = resp.Uids[funcName]
	}

	log.Printf("Created %d function nodes", len(uidMap))

	// Second pass: Create relationships
	log.Println("Creating function call relationships...")
	relationshipCount := 0
	for funcName, function := range functionMap {
		var nquads strings.Builder
		for _, calledFunc := range function.Calls {
			if calledUid, ok := uidMap[calledFunc]; ok {
				fmt.Fprintf(&nquads, "<%s> <calls> <%s> .\n", uidMap[funcName], calledUid)
				relationshipCount++
			}
		}

		if nquads.Len() > 0 {
			mutation := &api.Mutation{
				SetNquads: []byte(nquads.String()),
			}

			_, err := client.NewTxn().Mutate(context.Background(), mutation)
			if err != nil {
				return fmt.Errorf("error creating relationships: %v", err)
			}
		}
	}

	log.Printf("Created %d function call relationships", relationshipCount)
	return nil
}
