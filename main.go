package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/goccy/go-graphviz"
	_ "github.com/mattn/go-sqlite3"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

type Function struct {
	Name     string   `json:"name"`
	FilePath string   `json:"filePath"`
	Calls    []string `json:"calls"`
}

func main() {
	log.Printf("Running")

	outputPath := flag.String("o", "callgraph.dot", "Output DOT file path")
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("Usage: program [-o output_file] <source_directory>")
	}
	sourceDir := flag.Arg(0)

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
	// err = storeToDgraph(functionMap)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// log.Println("Data storage complete")

	// log.Println("Querying stored data...")
	// result, err := queryCallGraph()
	// if err != nil {
	// 	log.Fatal(err)
	// }
	log.Println("Query complete")

	log.Println("Generating DOT file...")
	dotBuf, err := generateDotOutput(functionMap)
	if err != nil {
		log.Fatal(err)
	}
	err = writeDotFile(dotBuf, *outputPath)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("DOT file generation complete")
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
	// Open SQLite database
	log.Println("Opening SQLite database...")
	db, err := sql.Open("sqlite3", "callgraph.db")
	if err != nil {
		return fmt.Errorf("error opening SQLite database: %v", err)
	}
	defer db.Close()

	// Create tables
	log.Println("Creating tables...")
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS nodes (
			body TEXT,
			id   TEXT GENERATED ALWAYS AS (json_extract(body, '$.id')) VIRTUAL NOT NULL UNIQUE
		);

		CREATE INDEX IF NOT EXISTS id_idx ON nodes(id);

		CREATE TABLE IF NOT EXISTS edges (
			source     TEXT,
			target     TEXT,
			properties TEXT,
			UNIQUE(source, target, properties) ON CONFLICT REPLACE,
			FOREIGN KEY(source) REFERENCES nodes(id),
			FOREIGN KEY(target) REFERENCES nodes(id)
		);

		CREATE INDEX IF NOT EXISTS source_idx ON edges(source);
		CREATE INDEX IF NOT EXISTS target_idx ON edges(target);
	`)
	if err != nil {
		return fmt.Errorf("error creating tables: %v", err)
	}

	// Insert nodes
	log.Println("Inserting function nodes...")
	for _, function := range functionMap {
		body, err := json.Marshal(map[string]string{
			"id":       function.Name,
			"name":     function.Name,
			"filePath": function.FilePath,
		})
		if err != nil {
			return fmt.Errorf("error marshaling function data: %v", err)
		}

		_, err = db.Exec("INSERT OR REPLACE INTO nodes (body) VALUES (?)", string(body))
		if err != nil {
			return fmt.Errorf("error inserting node: %v", err)
		}
	}

	// Insert edges
	log.Println("Inserting function call relationships...")
	for funcName, function := range functionMap {
		for _, calledFunc := range function.Calls {
			_, err = db.Exec("INSERT OR REPLACE INTO edges (source, target, properties) VALUES (?, ?, ?)",
				funcName, calledFunc, "calls")
			if err != nil {
				return fmt.Errorf("error inserting edge: %v", err)
			}
		}
	}

	log.Printf("Inserted %d functions", len(functionMap))
	return nil
}

func queryCallGraph() (map[string]*Function, error) {
	// Open SQLite database
	db, err := sql.Open("sqlite3", "callgraph.db")
	if err != nil {
		return nil, fmt.Errorf("error opening SQLite database: %v", err)
	}
	defer db.Close()

	// Query to fetch all functions and their calls
	rows, err := db.Query(`
		SELECT n.body, COALESCE(e.target, '') as target
		FROM nodes n
		LEFT JOIN edges e ON n.id = e.source
		ORDER BY n.id, e.target
	`)
	if err != nil {
		return nil, fmt.Errorf("error querying SQLite: %v", err)
	}
	defer rows.Close()

	result := make(map[string]*Function)
	for rows.Next() {
		var body, target string
		err := rows.Scan(&body, &target)
		if err != nil {
			return nil, fmt.Errorf("error scanning row: %v", err)
		}

		var funcData Function
		err = json.Unmarshal([]byte(body), &funcData)
		if err != nil {
			return nil, fmt.Errorf("error unmarshaling function data: %v", err)
		}

		if _, ok := result[funcData.Name]; !ok {
			result[funcData.Name] = &funcData
			result[funcData.Name].Calls = make([]string, 0)
		}

		if target != "" {
			result[funcData.Name].Calls = append(result[funcData.Name].Calls, target)
		}
	}

	return result, nil
}

func generateDotOutput(result map[string]*Function) (*bytes.Buffer, error) {
	// Create graphviz graph
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("error creating graphviz: %v", err)
	}
	defer g.Close()

	graph, err := g.Graph()
	if err != nil {
		return nil, fmt.Errorf("error creating graph: %v", err)
	}
	defer func() {
		if err := graph.Close(); err != nil {
			log.Printf("error closing graph: %v", err)
		}
	}()

	// Create nodes and edges
	nodes := make(map[string]*graphviz.Node)
	for funcName, funcData := range result {
		node, err := graph.CreateNodeByName(funcName)
		if err != nil {
			return nil, fmt.Errorf("error creating node: %v", err)
		}
		nodes[funcName] = node

		for _, calledFunc := range funcData.Calls {
			if _, ok := nodes[calledFunc]; !ok {
				calledNode, err := graph.CreateNodeByName(calledFunc)
				if err != nil {
					return nil, fmt.Errorf("error creating node: %v", err)
				}
				nodes[calledFunc] = calledNode
			}
			_, err := graph.CreateEdgeByName("call", nodes[funcName], nodes[calledFunc])
			if err != nil {
				return nil, fmt.Errorf("error creating edge: %v", err)
			}
		}
	}

	// Generate DOT output
	var dotBuf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.Format(graphviz.DOT), &dotBuf); err != nil {
		return nil, fmt.Errorf("error rendering DOT output: %v", err)
	}

	return &dotBuf, nil
}

func writeDotFile(dotBuf *bytes.Buffer, outputPath string) error {
	// Write the dot output buffer to a file
	if err := os.WriteFile(outputPath, dotBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("error writing DOT output to file: %v", err)
	}

	fmt.Printf("\nDOT file saved to: %s\n", outputPath)
	return nil
}
