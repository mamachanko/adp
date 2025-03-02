package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/ledongthuc/pdf"
	"github.com/spf13/cobra"
)

// NewProcessCmd creates and configures the process command
func NewProcessCmd(config Config) *cobra.Command {
	var pdfPath string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "process",
		Short: "Process downloaded PDFs",
		Long:  `Process all downloaded PDFs from ADP and extract relevant information.`,
		Run: func(cmd *cobra.Command, args []string) {
			// Validate directory exists
			if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
				log.Error("Directory does not exist", "path", pdfPath)
				os.Exit(1)
			}

			log.Info("Starting PDF processing", "path", pdfPath, "dry_run", dryRun)

			// Run the processor
			if err := processPDFs(pdfPath, dryRun); err != nil {
				log.Error("Error processing PDFs", "error", err)
				os.Exit(1)
			}

			log.Info("All PDFs processed successfully!")
		},
	}

	// Add path flag
	cmd.Flags().StringVar(&pdfPath, "path", config.DefaultDir, "Path to directory containing PDFs")
	cmd.Flags().BoolVar(&dryRun, "dry", false, "Dry run mode")

	return cmd
}

func processPDFs(pdfPath string, dryRun bool) error {
	// Find all PDF files in the directory
	pdfFiles, err := filepath.Glob(filepath.Join(pdfPath, "*.pdf"))
	if err != nil {
		return fmt.Errorf("failed to list PDF files: %v", err)
	}

	log.Info("Found PDF files", "count", len(pdfFiles))

	// Regex to match tax certificate and extract year
	taxCertRegex := regexp.MustCompile(`Ausdruck der elektronischen Lohnsteuerbescheinigung für (\d{4})`)

	// Regex to match social insurance certificate and extract month and year
	socialInsuranceRegex := regexp.MustCompile(`Meldebescheinigung zur Sozialversicherung`)

	// Regex to match payslip and extract month and year
	payslipRegex := regexp.MustCompile(`Verdienstabrechnung`)

	// Regex to detect Rückrechnung in payslips
	rueckrechnungRegex := regexp.MustCompile(`Rückrechnung:?\s*([A-Za-zäöüÄÖÜß]+)\s+(\d{4})`)

	// Common regex for extracting month and year from Abrechnungsmonat
	abrechnungsmonatRegex := regexp.MustCompile(`Abrechnungsmonat:?\s*([A-Za-zäöüÄÖÜß]+)\s+(\d{4})`)

	// Process each PDF file
	for i, pdfFile := range pdfFiles {
		filename := filepath.Base(pdfFile)
		log.Info("Processing PDF",
			"number", fmt.Sprintf("%d/%d", i+1, len(pdfFiles)),
			"filename", filename)

		// Extract text from PDF
		text, err := extractTextFromPDF(pdfFile)
		if err != nil {
			log.Warn("Failed to extract text from PDF", "filename", filename, "error", err)
			continue
		}

		// Check if it's a tax certificate
		matches := taxCertRegex.FindStringSubmatch(text)
		if len(matches) > 1 {
			year := matches[1]
			newFilename := fmt.Sprintf("Lohnsteuerbescheinigung - %s.pdf", year)
			newPath := filepath.Join(pdfPath, newFilename)

			// Ensure the new filename doesn't overwrite an existing file
			newPath = ensureUniqueFilename(newPath)
			newFilename = filepath.Base(newPath)

			log.Info("Found tax certificate",
				"filename", filename,
				"year", year,
				"new_filename", newFilename)

			if dryRun {
				log.Info("Would rename", "filename", filename, "new_filename", newFilename)
			} else {
				if err := os.Rename(pdfFile, newPath); err != nil {
					log.Error("Failed to rename file", "filename", filename, "error", err)
					continue
				}
				log.Info("Renamed file successfully", "old", filename, "new", newFilename)
			}
		} else if socialInsuranceRegex.MatchString(text) {
			// Check if it's a social insurance certificate
			monthYearMatches := abrechnungsmonatRegex.FindStringSubmatch(text)
			if len(monthYearMatches) > 2 {
				month := monthYearMatches[1]
				year := monthYearMatches[2]
				newFilename := fmt.Sprintf("Meldebescheinigung zur Sozialversicherung - %s %s.pdf", month, year)
				newPath := filepath.Join(pdfPath, newFilename)

				// Ensure the new filename doesn't overwrite an existing file
				newPath = ensureUniqueFilename(newPath)
				newFilename = filepath.Base(newPath)

				log.Info("Found social insurance certificate",
					"filename", filename,
					"month", month,
					"year", year,
					"new_filename", newFilename)

				if dryRun {
					log.Info("Would rename", "filename", filename, "new_filename", newFilename)
				} else {
					if err := os.Rename(pdfFile, newPath); err != nil {
						log.Error("Failed to rename file", "filename", filename, "error", err)
						continue
					}
					log.Info("Renamed file successfully", "old", filename, "new", newFilename)
				}
			} else {
				log.Warn("Found social insurance certificate but couldn't extract month/year",
					"filename", filename)
			}
		} else if payslipRegex.MatchString(text) {
			// Check if it's a payslip
			monthYearMatches := abrechnungsmonatRegex.FindStringSubmatch(text)
			if len(monthYearMatches) > 2 {
				month := monthYearMatches[1]
				year := monthYearMatches[2]

				// Check if it's a Rückrechnung
				rueckrechnungMatches := rueckrechnungRegex.FindStringSubmatch(text)
				var newFilename string
				if len(rueckrechnungMatches) > 2 {
					// It's a Rückrechnung payslip
					rueckMonth := rueckrechnungMatches[1]
					rueckYear := rueckrechnungMatches[2]
					newFilename = fmt.Sprintf("Verdienstabrechnung - %s %s - Rückrechnung.pdf", rueckMonth, rueckYear)
				} else {
					// Regular payslip
					newFilename = fmt.Sprintf("Verdienstabrechnung - %s %s.pdf", month, year)
				}

				newPath := filepath.Join(pdfPath, newFilename)

				// Ensure the new filename doesn't overwrite an existing file
				newPath = ensureUniqueFilename(newPath)
				newFilename = filepath.Base(newPath)

				log.Info("Found payslip",
					"filename", filename,
					"month", month,
					"year", year,
					"new_filename", newFilename)

				if dryRun {
					log.Info("Would rename", "filename", filename, "new_filename", newFilename)
				} else {
					if err := os.Rename(pdfFile, newPath); err != nil {
						log.Error("Failed to rename file", "filename", filename, "error", err)
						continue
					}
					log.Info("Renamed file successfully", "old", filename, "new", newFilename)
				}
			} else {
				log.Warn("Found payslip but couldn't extract month/year",
					"filename", filename)
			}
		} else {
			log.Info("Not a recognized certificate type", "filename", filename)
		}
	}

	return nil
}

// ensureUniqueFilename ensures the given path doesn't overwrite an existing file
// by adding "_2" suffix if needed
func ensureUniqueFilename(path string) string {
	// If the file doesn't exist, return the original path
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}

	// File exists, add "_2" suffix
	ext := filepath.Ext(path)
	basePath := path[:len(path)-len(ext)]
	return fmt.Sprintf("%s_2%s", basePath, ext)
}

// extractTextFromPDF extracts text content from a PDF file
func extractTextFromPDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf strings.Builder
	b, err := r.GetPlainText()
	if err != nil {
		return "", err
	}

	_, err = io.Copy(&buf, b)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
