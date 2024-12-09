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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	log.Println("Querying stored data...")
	err = queryDgraph()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Query complete")
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

	// Execute the query:
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

	// Set schema with retry
	log.Println("Setting Dgraph schema...")
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
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
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "Pending transactions found") {
			log.Printf("Pending transactions found. Retrying in %d seconds...", i+1)
			time.Sleep(time.Duration(i+1) * time.Second)
		} else {
			return fmt.Errorf("error setting schema: %v", err)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to set schema after %d retries: %v", maxRetries, err)
	}

	// Start a new transaction
	txn := client.NewTxn()
	defer txn.Discard(context.Background())

	// Upsert all functions
	log.Println("Upserting function nodes...")
	for _, function := range functionMap {
		mutation := &api.Mutation{
			SetNquads: []byte(fmt.Sprintf(`
				uid(func) <name> %q .
				uid(func) <filePath> %q .
				uid(func) <dgraph.type> "Function" .
			`, function.Name, function.FilePath)),
			Cond: fmt.Sprintf(`
				@if(eq(len(func), 0)) {
					func as var(func: type(Function)) @filter(eq(name, %q) AND eq(filePath, %q))
				} @else {
					func as var(func: type(Function)) @filter(eq(name, %q) AND eq(filePath, %q))
				}
			`, function.Name, function.FilePath, function.Name, function.FilePath),
		}

		_, err := txn.Mutate(context.Background(), mutation)
		if err != nil {
			return fmt.Errorf("error upserting function node: %v", err)
		}
	}

	// Query to get all function UIDs
	const q = `
		{
			functions(func: type(Function)) {
				uid
				name
			}
		}
	`

	resp, err := txn.Query(context.Background(), q)
	if err != nil {
		return fmt.Errorf("error querying function UIDs: %v", err)
	}

	var result struct {
		Functions []struct {
			UID  string `json:"uid"`
			Name string `json:"name"`
		} `json:"functions"`
	}

	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return fmt.Errorf("error unmarshaling query result: %v", err)
	}

	uidMap := make(map[string]string)
	for _, f := range result.Functions {
		uidMap[f.Name] = f.UID
	}

	// Clear existing relationships
	log.Println("Clearing existing function call relationships...")
	for funcName := range functionMap {
		if uid, ok := uidMap[funcName]; ok {
			mutation := &api.Mutation{
				DelNquads: []byte(fmt.Sprintf("<%s> <calls> * .", uid)),
			}
			_, err := txn.Mutate(context.Background(), mutation)
			if err != nil {
				return fmt.Errorf("error clearing existing relationships: %v", err)
			}
		}
	}

	// Update relationships
	log.Println("Updating function call relationships...")
	for funcName, function := range functionMap {
		var nquads strings.Builder
		for _, calledFunc := range function.Calls {
			if calledUid, ok := uidMap[calledFunc]; ok {
				fmt.Fprintf(&nquads, "<%s> <calls> <%s> .\n", uidMap[funcName], calledUid)
			}
		}

		if nquads.Len() > 0 {
			mutation := &api.Mutation{
				SetNquads: []byte(nquads.String()),
			}

			_, err := txn.Mutate(context.Background(), mutation)
			if err != nil {
				return fmt.Errorf("error updating relationships: %v", err)
			}
		}
	}

	// Commit the transaction
	err = txn.Commit(context.Background())
	if err != nil {
		return fmt.Errorf("error committing transaction: %v", err)
	}

	log.Printf("Upserted %d functions", len(functionMap))
	return nil
}

func queryDgraph() error {
	// Connect to Dgraph
	conn, err := grpc.Dial("localhost:9080", grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("error connecting to Dgraph: %v", err)
	}
	defer conn.Close()

	client := dgo.NewDgraphClient(api.NewDgraphClient(conn))

	// Query to fetch all functions and their calls
	const q = `
	{
		functions(func: has(name)) {
			name
			filePath
			calls {
				name
			}
		}
	}
	`

	resp, err := client.NewTxn().Query(context.Background(), q)
	if err != nil {
		return fmt.Errorf("error querying Dgraph: %v", err)
	}

	fmt.Println("Query Result:")
	fmt.Println(string(resp.Json))
	var prettyJSON bytes.Buffer
	error := json.Indent(&prettyJSON, resp.Json, "", "    ")
	if error != nil {
		return error
	}

	// fmt.Println(prettyJSON.String())
	return nil
}
