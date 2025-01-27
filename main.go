// Cli tool for generating call graph of Go code.
// It uses Tree-sitter to parse Go code and generate a DOT file representing the call graph.
// The generated DOT file can be visualized using Graphviz tools like `dot` or `xdot`.

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/goccy/go-graphviz"
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

	dotFilePath := flag.String("dotgraph", "", "Output DOT file path")
	graphmlFilePath := flag.String("graphml", "", "Output GraphML file path")
	gexfFilePath := flag.String("gexf", "", "Output GEXF file path")
	pngFilePath := flag.String("png", "", "Output PNG file path")
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("Usage: program [-dotgraph output_file] [-graphml output_file] [-gexf output_file] [-png output_file] <source_directory>")
	}
	if *dotFilePath == "" && *graphmlFilePath == "" && *gexfFilePath == "" && *pngFilePath == "" {
		log.Fatal("At least one of -dotgraph, -graphml, -gexf, or -png must be set")
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

	var graph *graphviz.Graph
	if *dotFilePath != "" || *pngFilePath != "" {
		var dotBuf *bytes.Buffer
		var err error
		graph, dotBuf, err = generateDotOutput(functionMap)
		if err != nil {
			log.Fatal(err)
		}
		if *dotFilePath != "" {
			log.Println("Generating DOT file...")
			err = writeDotFile(dotBuf, *dotFilePath)
			if err != nil {
				log.Fatal(err)
			}
			log.Println("DOT file generation complete")
		}
	}

	if *pngFilePath != "" {
		log.Println("Generating PNG file...")
		err := generatePNGOutput(graph, *pngFilePath)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("PNG file generation complete")
	}

	if *graphmlFilePath != "" {
		log.Println("Generating GraphML file...")
		graphmlBuf, err := generateGraphMLOutput(functionMap)
		if err != nil {
			log.Fatal(err)
		}
		err = writeGraphMLFile(graphmlBuf, *graphmlFilePath)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("GraphML file generation complete")
	}

	if *gexfFilePath != "" {
		log.Println("Generating GEXF file...")
		gexfBuf, err := generateGEXFOutput(functionMap)
		if err != nil {
			log.Fatal(err)
		}
		err = writeGEXFFile(gexfBuf, *gexfFilePath)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("GEXF file generation complete")
	}
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

func generateDotOutput(result map[string]*Function) (*graphviz.Graph, *bytes.Buffer, error) {
	// Create graphviz graph
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating graphviz: %v", err)
	}
	defer g.Close()

	graph, err := g.Graph()
	if err != nil {
		return nil, nil, fmt.Errorf("error creating graph: %v", err)
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
			return nil, nil, fmt.Errorf("error creating node: %v", err)
		}
		nodes[funcName] = node

		for _, calledFunc := range funcData.Calls {
			if _, ok := nodes[calledFunc]; !ok {
				calledNode, err := graph.CreateNodeByName(calledFunc)
				if err != nil {
					return nil, nil, fmt.Errorf("error creating node: %v", err)
				}
				nodes[calledFunc] = calledNode
			}
			_, err := graph.CreateEdgeByName("call", nodes[funcName], nodes[calledFunc])
			if err != nil {
				return nil, nil, fmt.Errorf("error creating edge: %v", err)
			}
		}
	}

	// Generate DOT output
	var dotBuf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.Format(graphviz.DOT), &dotBuf); err != nil {
		return nil, nil, fmt.Errorf("error rendering DOT output: %v", err)
	}

	return graph, &dotBuf, nil
}

func writeDotFile(dotBuf *bytes.Buffer, outputPath string) error {
	// Write the dot output buffer to a file
	if err := os.WriteFile(outputPath, dotBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("error writing DOT output to file: %v", err)
	}

	fmt.Printf("\nDOT file saved to: %s\n", outputPath)
	return nil
}

func generateGraphMLOutput(result map[string]*Function) (*bytes.Buffer, error) {
	var graphmlBuf bytes.Buffer
	graphmlBuf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	graphmlBuf.WriteString(`<graphml xmlns="http://graphml.graphdrawing.org/xmlns">`)
	graphmlBuf.WriteString(`<graph id="G" edgedefault="directed">`)

	for funcName, funcData := range result {
		graphmlBuf.WriteString(fmt.Sprintf(`<node id="%s"><data key="label">%s</data></node>`, funcName, funcName))
		for _, calledFunc := range funcData.Calls {
			graphmlBuf.WriteString(fmt.Sprintf(`<edge source="%s" target="%s"/>`, funcName, calledFunc))
		}
	}

	graphmlBuf.WriteString(`</graph>`)
	graphmlBuf.WriteString(`</graphml>`)

	return &graphmlBuf, nil
}

func writeGraphMLFile(graphmlBuf *bytes.Buffer, outputPath string) error {
	if err := os.WriteFile(outputPath, graphmlBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("error writing GraphML output to file: %v", err)
	}

	fmt.Printf("\nGraphML file saved to: %s\n", outputPath)
	return nil
}

func generateGEXFOutput(result map[string]*Function) (*bytes.Buffer, error) {
	var gexfBuf bytes.Buffer
	gexfBuf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	gexfBuf.WriteString(`<gexf xmlns="http://www.gexf.net/1.2draft" version="1.2">`)
	gexfBuf.WriteString(`<graph mode="static" defaultedgetype="directed">`)
	gexfBuf.WriteString(`<nodes>`)

	for funcName := range result {
		gexfBuf.WriteString(fmt.Sprintf(`<node id="%s" label="%s"/>`, funcName, funcName))
	}

	gexfBuf.WriteString(`</nodes>`)
	gexfBuf.WriteString(`<edges>`)

	edgeID := 0
	for funcName, funcData := range result {
		for _, calledFunc := range funcData.Calls {
			gexfBuf.WriteString(fmt.Sprintf(`<edge id="%d" source="%s" target="%s"/>`, edgeID, funcName, calledFunc))
			edgeID++
		}
	}

	gexfBuf.WriteString(`</edges>`)
	gexfBuf.WriteString(`</graph>`)
	gexfBuf.WriteString(`</gexf>`)

	return &gexfBuf, nil
}

func writeGEXFFile(gexfBuf *bytes.Buffer, outputPath string) error {
	if err := os.WriteFile(outputPath, gexfBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("error writing GEXF output to file: %v", err)
	}

	fmt.Printf("\nGEXF file saved to: %s\n", outputPath)
	return nil
}

func generatePNGOutput(graph *graphviz.Graph, outputPath string) error {
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return fmt.Errorf("error creating graphviz: %v", err)
	}
	defer g.Close()
	if err := g.RenderFilename(ctx, graph, graphviz.PNG, outputPath); err != nil {
		return fmt.Errorf("error rendering PNG output: %v", err)
	}
	fmt.Printf("\nPNG file saved to: %s\n", outputPath)
	return nil
}
