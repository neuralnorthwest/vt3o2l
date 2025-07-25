package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

const (
	version = "1.0.0"
)

// Domain types representing the transcript data model
type Utterance struct {
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

type Chunk struct {
	Timestamp  string      `json:"timestamp"`
	StartTime  string      `json:"start_time"`
	EndTime    string      `json:"end_time"`
	Utterances []Utterance `json:"utterances"`
}

type TranscriptData struct {
	Chunks []Chunk `json:"chunks"`
}

// Parser interface for clean abstraction
type Parser interface {
	Parse(content []byte) (*TranscriptData, error)
}

// Writer interface for clean abstraction
type Writer interface {
	Write(data *TranscriptData) ([]byte, error)
}

// VTTParser implements Parser for WebVTT format
type VTTParser struct {
	timestampPattern *regexp.Regexp
	speakerPattern   *regexp.Regexp
}

func NewVTTParser() *VTTParser {
	return &VTTParser{
		timestampPattern: regexp.MustCompile(`^(\d{2}:\d{2}:\d{2}\.\d{3})\s*-->\s*(\d{2}:\d{2}:\d{2}\.\d{3})$`),
		speakerPattern:   regexp.MustCompile(`^\(([^)]+)\)$`),
	}
}

func (p *VTTParser) Parse(content []byte) (*TranscriptData, error) {
	contentStr := string(content)
	if !strings.HasPrefix(contentStr, "WEBVTT") {
		return nil, fmt.Errorf("invalid VTT file: missing WEBVTT header")
	}

	lines := strings.Split(contentStr, "\n")
	var chunks []Chunk
	var currentChunk *Chunk
	var currentTextLines []string
	inContent := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if matches := p.timestampPattern.FindStringSubmatch(line); matches != nil {
			inContent = true

			// Finalize previous chunk
			if currentChunk != nil {
				utterances := p.parseUtterances(currentTextLines)
				currentChunk.Utterances = utterances
				chunks = append(chunks, *currentChunk)
			}

			// Initialize new chunk
			currentChunk = &Chunk{
				Timestamp: line,
				StartTime: matches[1],
				EndTime:   matches[2],
			}
			currentTextLines = []string{}
		} else if inContent && line != "" {
			currentTextLines = append(currentTextLines, line)
		}
	}

	// Finalize last chunk
	if currentChunk != nil {
		utterances := p.parseUtterances(currentTextLines)
		currentChunk.Utterances = utterances
		chunks = append(chunks, *currentChunk)
	}

	return &TranscriptData{Chunks: chunks}, nil
}

func (p *VTTParser) parseUtterances(lines []string) []Utterance {
	var utterances []Utterance
	var currentSpeaker string
	var currentText []string

	finalizeUtterance := func() {
		if currentSpeaker != "" && len(currentText) > 0 {
			text := strings.TrimSpace(strings.Join(currentText, " "))
			utterances = append(utterances, Utterance{
				Speaker: currentSpeaker,
				Text:    text,
			})
		}
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if matches := p.speakerPattern.FindStringSubmatch(line); matches != nil {
			finalizeUtterance()
			currentSpeaker = matches[1]
			currentText = []string{}
		} else if line == "-" {
			finalizeUtterance()
			currentSpeaker = ""
			currentText = []string{}
		} else if currentSpeaker != "" {
			currentText = append(currentText, line)
		}
	}

	finalizeUtterance()
	return utterances
}

// JSONParser implements Parser for JSON format
type JSONParser struct{}

func NewJSONParser() *JSONParser {
	return &JSONParser{}
}

func (p *JSONParser) Parse(content []byte) (*TranscriptData, error) {
	var data TranscriptData
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON format: %w", err)
	}
	return &data, nil
}

// VTTWriter implements Writer for WebVTT format
type VTTWriter struct{}

func NewVTTWriter() *VTTWriter {
	return &VTTWriter{}
}

func (w *VTTWriter) Write(data *TranscriptData) ([]byte, error) {
	var builder strings.Builder

	builder.WriteString("WEBVTT\n")
	builder.WriteString("Kind: captions\n")
	builder.WriteString("Language: en\n\n")

	for _, chunk := range data.Chunks {
		builder.WriteString(chunk.Timestamp)
		builder.WriteString("\n")

		for i, utterance := range chunk.Utterances {
			if i > 0 {
				builder.WriteString("-\n\n")
			}
			builder.WriteString(fmt.Sprintf("(%s)\n", utterance.Speaker))
			builder.WriteString(utterance.Text)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	return []byte(builder.String()), nil
}

// JSONWriter implements Writer for JSON format
type JSONWriter struct {
	indent bool
}

func NewJSONWriter(indent bool) *JSONWriter {
	return &JSONWriter{indent: indent}
}

func (w *JSONWriter) Write(data *TranscriptData) ([]byte, error) {
	if w.indent {
		return json.MarshalIndent(data, "", "  ")
	}
	return json.Marshal(data)
}

// Service layer for business logic
type TranscriptService struct {
	parsers map[string]Parser
	writers map[string]Writer
}

func NewTranscriptService() *TranscriptService {
	return &TranscriptService{
		parsers: map[string]Parser{
			".vtt":  NewVTTParser(),
			".json": NewJSONParser(),
		},
		writers: map[string]Writer{
			".vtt":  NewVTTWriter(),
			".json": NewJSONWriter(true),
		},
	}
}

func (s *TranscriptService) Convert(inputPath, outputPath string) error {
	// Read input file
	content, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Determine input format
	inputExt := strings.ToLower(filepath.Ext(inputPath))
	parser, exists := s.parsers[inputExt]
	if !exists {
		return fmt.Errorf("unsupported input format: %s", inputExt)
	}

	// Parse input
	data, err := parser.Parse(content)
	if err != nil {
		return fmt.Errorf("failed to parse input: %w", err)
	}

	// Determine output format
	outputExt := strings.ToLower(filepath.Ext(outputPath))
	writer, exists := s.writers[outputExt]
	if !exists {
		return fmt.Errorf("unsupported output format: %s", outputExt)
	}

	// Write output
	output, err := writer.Write(data)
	if err != nil {
		return fmt.Errorf("failed to generate output: %w", err)
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

func (s *TranscriptService) Validate(vttPath, jsonPath string) error {
	// Read and parse VTT
	vttContent, err := os.ReadFile(vttPath)
	if err != nil {
		return fmt.Errorf("failed to read VTT file: %w", err)
	}

	vttData, err := s.parsers[".vtt"].Parse(vttContent)
	if err != nil {
		return fmt.Errorf("failed to parse VTT file: %w", err)
	}

	// Read and parse JSON
	jsonContent, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to read JSON file: %w", err)
	}

	jsonData, err := s.parsers[".json"].Parse(jsonContent)
	if err != nil {
		return fmt.Errorf("failed to parse JSON file: %w", err)
	}

	// Compare structures
	return s.compareTranscripts(vttData, jsonData)
}

func (s *TranscriptService) compareTranscripts(vtt, json *TranscriptData) error {
	if len(vtt.Chunks) != len(json.Chunks) {
		return fmt.Errorf("chunk count mismatch: VTT=%d, JSON=%d", len(vtt.Chunks), len(json.Chunks))
	}

	for i, vttChunk := range vtt.Chunks {
		jsonChunk := json.Chunks[i]

		if vttChunk.Timestamp != jsonChunk.Timestamp {
			return fmt.Errorf("chunk %d timestamp mismatch", i)
		}

		if vttChunk.StartTime != jsonChunk.StartTime {
			return fmt.Errorf("chunk %d start time mismatch", i)
		}

		if vttChunk.EndTime != jsonChunk.EndTime {
			return fmt.Errorf("chunk %d end time mismatch", i)
		}

		if len(vttChunk.Utterances) != len(jsonChunk.Utterances) {
			return fmt.Errorf("chunk %d utterance count mismatch", i)
		}

		for j, vttUtt := range vttChunk.Utterances {
			jsonUtt := jsonChunk.Utterances[j]

			if vttUtt.Speaker != jsonUtt.Speaker {
				return fmt.Errorf("chunk %d, utterance %d speaker mismatch", i, j)
			}

			if vttUtt.Text != jsonUtt.Text {
				return fmt.Errorf("chunk %d, utterance %d text mismatch", i, j)
			}
		}
	}

	return nil
}

func (s *TranscriptService) GetStats(filePath string) (int, int, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	parser, exists := s.parsers[ext]
	if !exists {
		return 0, 0, fmt.Errorf("unsupported file format: %s", ext)
	}

	data, err := parser.Parse(content)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse file: %w", err)
	}

	chunkCount := len(data.Chunks)
	utteranceCount := 0
	for _, chunk := range data.Chunks {
		utteranceCount += len(chunk.Utterances)
	}

	return chunkCount, utteranceCount, nil
}

type Gap struct {
	Index     int
	Timestamp string
	StartTime string
	EndTime   string
	Duration  string
}

func (s *TranscriptService) FindGaps(filePath string) ([]Gap, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	parser, exists := s.parsers[ext]
	if !exists {
		return nil, fmt.Errorf("unsupported file format: %s", ext)
	}

	data, err := parser.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	var gaps []Gap
	for i, chunk := range data.Chunks {
		if len(chunk.Utterances) == 0 {
			duration := calculateDuration(chunk.StartTime, chunk.EndTime)
			gaps = append(gaps, Gap{
				Index:     i,
				Timestamp: chunk.Timestamp,
				StartTime: chunk.StartTime,
				EndTime:   chunk.EndTime,
				Duration:  duration,
			})
		}
	}

	return gaps, nil
}

func calculateDuration(start, end string) string {
	// Parse HH:MM:SS.mmm format
	startParts := strings.Split(strings.Replace(start, ".", ":", -1), ":")
	endParts := strings.Split(strings.Replace(end, ".", ":", -1), ":")

	if len(startParts) != 4 || len(endParts) != 4 {
		return "unknown"
	}

	// Convert to total milliseconds for easier calculation
	startMs := parseTimeToMs(startParts)
	endMs := parseTimeToMs(endParts)

	if startMs == -1 || endMs == -1 {
		return "unknown"
	}

	durationMs := endMs - startMs
	if durationMs < 0 {
		return "invalid"
	}

	// Convert back to readable format
	seconds := durationMs / 1000

	if seconds < 60 {
		return fmt.Sprintf("%.1fs", float64(durationMs)/1000.0)
	}

	minutes := seconds / 60
	seconds = seconds % 60

	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func parseTimeToMs(parts []string) int {
	// parts: [HH, MM, SS, mmm]
	if len(parts) != 4 {
		return -1
	}

	var h, m, s, ms int
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
		return -1
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
		return -1
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &s); err != nil {
		return -1
	}
	if _, err := fmt.Sscanf(parts[3], "%d", &ms); err != nil {
		return -1
	}

	return h*3600000 + m*60000 + s*1000 + ms
}

// CLI Commands
func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "vt3o2l",
		Short:   "Bidirectional VTT ↔ JSON transcript converter",
		Long:    `A production-grade tool for converting between WebVTT and structured JSON transcript formats with timestamp-based chunking and speaker separation.`,
		Version: version,
	}

	cmd.AddCommand(
		newConvertCmd(),
		newValidateCmd(),
		newStatsCmd(),
		newGapsCmd(),
	)

	return cmd
}

func newConvertCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "convert <input_file>",
		Short: "Convert between VTT and JSON formats",
		Long: `Convert transcript files between WebVTT (.vtt) and JSON (.json) formats.
The output format is automatically determined from the output file extension.
If no output file is specified, the input filename with swapped extension is used.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputPath := args[0]

			if outputPath == "" {
				outputPath = generateOutputPath(inputPath)
			}

			service := NewTranscriptService()
			if err := service.Convert(inputPath, outputPath); err != nil {
				return err
			}

			chunks, utterances, _ := service.GetStats(outputPath)
			fmt.Printf("✓ Converted %s → %s\n", inputPath, outputPath)
			fmt.Printf("  %d chunks, %d utterances\n", chunks, utterances)

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (auto-generated if not specified)")

	return cmd
}

func newValidateCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "validate <vtt_file> <json_file>",
		Short: "Validate consistency between VTT and JSON files",
		Long:  `Verify that a VTT file and its corresponding JSON representation contain identical transcript data.`,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vttPath, jsonPath := args[0], args[1]

			service := NewTranscriptService()

			if verbose {
				fmt.Printf("Validating %s ↔ %s...\n", vttPath, jsonPath)
			}

			if err := service.Validate(vttPath, jsonPath); err != nil {
				fmt.Printf("✗ VALIDATION FAILED: %v\n", err)
				return fmt.Errorf("validation failed")
			}

			chunks, utterances, _ := service.GetStats(vttPath)
			fmt.Printf("✓ VALIDATION PASSED: Files are consistent\n")
			fmt.Printf("  %d chunks, %d utterances validated\n", chunks, utterances)

			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed validation progress")

	return cmd
}

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats <file>",
		Short: "Show transcript statistics",
		Long:  `Display chunk and utterance counts for a transcript file.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]

			service := NewTranscriptService()
			chunks, utterances, err := service.GetStats(filePath)
			if err != nil {
				return err
			}

			fmt.Printf("File: %s\n", filePath)
			fmt.Printf("Chunks: %d\n", chunks)
			fmt.Printf("Utterances: %d\n", utterances)

			return nil
		},
	}

	return cmd
}

func newGapsCmd() *cobra.Command {
	var showAll bool
	var maxResults int

	cmd := &cobra.Command{
		Use:   "gaps <file>",
		Short: "Find timestamp gaps with no utterances",
		Long: `Identify chunks in the transcript that contain timestamps but no speaker utterances.
These gaps represent silence periods or sections that would be removed during conversion.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]

			service := NewTranscriptService()
			gaps, err := service.FindGaps(filePath)
			if err != nil {
				return err
			}

			if len(gaps) == 0 {
				fmt.Printf("✓ No gaps found in %s\n", filePath)
				return nil
			}

			fmt.Printf("Found %d gap(s) in %s:\n\n", len(gaps), filePath)

			displayCount := len(gaps)
			if !showAll && maxResults > 0 && len(gaps) > maxResults {
				displayCount = maxResults
			}

			for i := 0; i < displayCount; i++ {
				gap := gaps[i]
				fmt.Printf("Gap #%d (chunk %d):\n", i+1, gap.Index+1)
				fmt.Printf("  Timestamp: %s\n", gap.Timestamp)
				fmt.Printf("  Duration:  %s\n", gap.Duration)
				fmt.Printf("  Content:   [empty - no utterances]\n\n")
			}

			if !showAll && maxResults > 0 && len(gaps) > maxResults {
				remaining := len(gaps) - maxResults
				fmt.Printf("... and %d more gap(s). Use --all to show all gaps.\n", remaining)
			}

			totalDuration := 0
			for _, gap := range gaps {
				if gap.Duration != "unknown" && gap.Duration != "invalid" {
					// Simple heuristic: assume most gaps are 4 seconds (typical chunk size)
					totalDuration += 4
				}
			}

			fmt.Printf("Summary:\n")
			fmt.Printf("  Total gaps: %d\n", len(gaps))
			fmt.Printf("  Estimated silence: ~%ds\n", totalDuration)
			fmt.Printf("  These gaps will be normalized during VTT ↔ JSON conversion.\n")

			return nil
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all gaps (default: limit to 10)")
	cmd.Flags().IntVar(&maxResults, "limit", 10, "Maximum number of gaps to display")

	return cmd
}

// Utility functions
func generateOutputPath(inputPath string) string {
	ext := filepath.Ext(inputPath)
	base := strings.TrimSuffix(inputPath, ext)

	switch strings.ToLower(ext) {
	case ".vtt":
		return base + ".json"
	case ".json":
		return base + ".vtt"
	default:
		return base + "_converted" + ext
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
