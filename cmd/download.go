package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/spf13/cobra"
)

// NewDownloadCmd creates and configures the download command
func NewDownloadCmd(config Config) *cobra.Command {
	var (
		siteURL      string
		username     string
		password     string
		downloadPath string
		headless     bool
		timeout      int
	)

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download PDFs from ADP",
		Long:  `Download all PDFs from adpworld.adp.com after logging in with provided credentials.`,
		Run: func(cmd *cobra.Command, args []string) {
			// Create download directory if it doesn't exist
			if err := os.MkdirAll(downloadPath, 0755); err != nil {
				log.Error("Failed to create download directory", "error", err)
				os.Exit(1)
			}

			log.Info("Starting ADP PDF downloader",
				"url", siteURL,
				"download_path", downloadPath,
				"timeout_minutes", timeout)

			// Run the downloader
			if err := downloadPDFs(siteURL, username, password, downloadPath, headless, timeout); err != nil {
				log.Error("Error downloading PDFs", "error", err)
				os.Exit(1)
			}

			log.Info("All PDFs downloaded successfully!")
		},
	}

	// Add flags specific to the download command
	cmd.Flags().StringVar(&siteURL, "url", "https://adpworld.adp.com", "ADP website URL")
	cmd.Flags().StringVarP(&username, "username", "u", os.Getenv("ADP_USERNAME"), "ADP username (required if ADP_USERNAME env var not set)")
	cmd.Flags().StringVarP(&password, "password", "p", os.Getenv("ADP_PASSWORD"), "ADP password (required if ADP_PASSWORD env var not set)")
	cmd.Flags().BoolVar(&headless, "headless", true, "Run browser in headless mode (no UI)")
	cmd.Flags().StringVar(&downloadPath, "download-path", config.DefaultDir, "Path to download PDFs")
	cmd.Flags().IntVar(&timeout, "timeout", 15, "Timeout in minutes for the entire operation")

	// Mark flags as required only if environment variables are not set
	if os.Getenv("ADP_USERNAME") == "" {
		cmd.MarkFlagRequired("username")
	}
	if os.Getenv("ADP_PASSWORD") == "" {
		cmd.MarkFlagRequired("password")
	}

	return cmd
}

// waitForElement waits for an element to be visible with custom timeout and polling
func waitForElement(ctx context.Context, selector string, timeout time.Duration) error {
	log.Debug("Waiting for element", "selector", selector, "timeout", timeout)

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Start polling
	start := time.Now()
	for {
		// Check if context is done
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timed out after %v waiting for element: %s", timeout, selector)
		default:
			// Continue
		}

		// Check if element is visible
		var visible bool
		err := chromedp.Run(timeoutCtx, chromedp.Evaluate(`
			(function() {
				const el = document.querySelector(`+"`"+selector+"`"+`);
				return el !== null && 
					(el.offsetWidth > 0 || el.offsetHeight > 0 || el.getClientRects().length > 0);
			})()
		`, &visible))

		// If evaluation failed, wait and retry
		if err != nil {
			if strings.Contains(err.Error(), "context deadline exceeded") {
				return fmt.Errorf("timed out waiting for element: %s", selector)
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// If element is visible, return success
		if visible {
			elapsed := time.Since(start)
			log.Debug("Element found", "selector", selector, "elapsed", elapsed)
			return nil
		}

		// Wait before next check
		time.Sleep(500 * time.Millisecond)
	}
}

// waitForText waits for text matching a regex pattern to appear on the page
func waitForText(ctx context.Context, pattern string, timeout time.Duration) error {
	log.Debug("Waiting for text matching pattern", "pattern", pattern, "timeout", timeout)

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Compile the regex
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex pattern: %v", err)
	}

	// Start polling
	start := time.Now()
	for {
		// Check if context is done
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timed out after %v waiting for text matching: %s", timeout, pattern)
		default:
			// Continue
		}

		// Get the page text content
		var pageText string
		err := chromedp.Run(timeoutCtx, chromedp.Evaluate(`
			(function() {
				return document.body.innerText;
			})()
		`, &pageText))

		// If evaluation failed, wait and retry
		if err != nil {
			if strings.Contains(err.Error(), "context deadline exceeded") {
				return fmt.Errorf("timed out waiting for text matching: %s", pattern)
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Check if the text matches the pattern
		if regex.MatchString(pageText) {
			elapsed := time.Since(start)
			log.Debug("Text matching pattern found", "pattern", pattern, "elapsed", elapsed)
			return nil
		}

		// Wait before next check
		time.Sleep(500 * time.Millisecond)
	}
}

func downloadPDFs(siteURL, username, password, downloadPath string, headless bool, timeoutMinutes int) error {
	// Create a new Chrome instance with incognito mode
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("incognito", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("disable-background-networking", false),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		// // Set locale to German
		chromedp.Env("LANGUAGE=de"),
		chromedp.Flag("lang", "de-DE"),
		chromedp.Flag("accept-language", "de-DE"),
		// Increase timeouts for slow connections
		chromedp.Flag("browser-test-mode", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	// Create a new browser with longer timeout
	ctx, cancel := chromedp.NewContext(
		allocCtx,
		chromedp.WithLogf(func(format string, args ...interface{}) {
			log.Debug(fmt.Sprintf(format, args...), "source", "chromedp")
		}),
	)
	defer cancel()

	// Set a timeout for the entire operation
	ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMinutes)*time.Minute)
	defer cancel()

	// Step 1: Navigate to the login page
	log.Info("Navigating to login page")
	if err := chromedp.Run(ctx, chromedp.Navigate(siteURL)); err != nil {
		return fmt.Errorf("failed to navigate to login page: %v", err)
	}

	// Step 2: Input username with more resilient waiting
	if err := waitForElement(ctx, "#login-form_username", 30*time.Second); err != nil {
		return fmt.Errorf("failed to find username field: %v", err)
	}

	log.Info("Entering username")
	if err := chromedp.Run(ctx,
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate(`
			(function() {
				const usernameField = document.querySelector("#login-form_username");
				if (usernameField && usernameField.shadowRoot) {
					const input = usernameField.shadowRoot.querySelector("#input");
					if (input) {
						input.focus();
						input.value = "`+username+`";
						input.dispatchEvent(new Event('input', { bubbles: true }));
						input.dispatchEvent(new Event('change', { bubbles: true }));
						return true;
					}
				}
				return false;
			})()
		`, nil),
		chromedp.Sleep(1*time.Second),
		chromedp.Click(`#verifUseridBtn`, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("failed to input username: %v", err)
	}

	// Step 3: Input password with more resilient waiting
	if err := waitForElement(ctx, "#login-form_password", 30*time.Second); err != nil {
		return fmt.Errorf("failed to find password field: %v", err)
	}

	log.Info("Entering password")
	if err := chromedp.Run(ctx,
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate(`
			(function() {
				const passwordField = document.querySelector("#login-form_password");
				if (passwordField && passwordField.shadowRoot) {
					const input = passwordField.shadowRoot.querySelector("#input");
					if (input) {
						input.focus();
						input.value = "`+password+`";
						input.dispatchEvent(new Event('input', { bubbles: true }));
						input.dispatchEvent(new Event('change', { bubbles: true }));
						return true;
					}
				}
				return false;
			})()
		`, nil),
		chromedp.Sleep(1*time.Second),
		chromedp.Click(`#signBtn`, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("failed to input password: %v", err)
	}

	log.Info("Logged in successfully")

	// Step 4: Navigate to All Documents page
	log.Info("Waiting for dashboard to load")

	if err := waitForText(ctx, "Alle Dokumente \\(\\d+\\)", 30*time.Second); err != nil {
		return fmt.Errorf("failed to find 'Alle Dokumente' button: %v", err)
	}

	log.Info("Navigating to All Documents page")
	if err := chromedp.Run(ctx,
		// TODO: more resilient
		// chromedp.WaitVisible("#ePayslipTile\\:ePayTileForm\\:j_idt568", chromedp.ByQuery),
		chromedp.Sleep(3*time.Second),
		// Find and click the "Alle Dokumente" button using JavaScript
		chromedp.Evaluate(`
			(function() {
				// Find all buttons, links, or elements with role="button"
				const elements = document.querySelectorAll('button, a, [role="button"]');
				// Find the first one containing "Alle Dokumente"
				for (const el of elements) {
					if (el.textContent.includes("Alle Dokumente")) {
						el.click();
						return true;
					}
				}
				return false;
			})()
		`, nil),
		// Wait a bit for navigation to complete
		chromedp.Sleep(2*time.Second),
	); err != nil {
		return fmt.Errorf("failed to find and click 'Alle Dokumente' button: %v", err)
	}

	// Step 5: Get cookies after navigating to the documents page
	log.Info("Getting cookies for document access")

	// Get all cookies from the browser using CDP
	var allCookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		allCookies, err = network.GetCookies().Do(ctx)
		return err
	})); err != nil {
		return fmt.Errorf("failed to get cookies from browser: %v", err)
	}

	// Create an HTTP client with cookies
	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("failed to create cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}

	// Parse the URL
	u, err := url.Parse(siteURL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %v", err)
	}

	// Only include the essential cookies
	var cookies []*http.Cookie
	essentialCookieNames := []string{
		"BIGipServer_DE1_world-v2",
		"SERVERSESSIONID",
		"JSESSIONIDSSO",
		"EMEASMSESSION",
	}

	for _, c := range allCookies {
		for _, name := range essentialCookieNames {
			if c.Name == name {
				cookies = append(cookies, &http.Cookie{
					Name:   c.Name,
					Value:  c.Value,
					Domain: c.Domain,
				})
				break
			}
		}
	}

	client.Jar.SetCookies(u, cookies)
	log.Info("Cookie setup complete", "cookie_count", len(cookies))

	// Find all PDF links
	pdfLinks, err := findPDFLinks(ctx)
	if err != nil {
		return fmt.Errorf("failed to find PDF links: %v", err)
	}

	log.Info("Found PDF links", "count", len(pdfLinks))

	// Download each PDF
	for i, link := range pdfLinks {
		// Ensure the link is absolute
		if !strings.HasPrefix(link, "https") {
			link = siteURL + link
		}

		log.Info("Downloading PDF",
			"number", fmt.Sprintf("%d/%d", i+1, len(pdfLinks)))

		filename := fmt.Sprintf("adp_%d.pdf", i+1)

		// Download the PDF
		if err := downloadFile(client, link, filepath.Join(downloadPath, filename)); err != nil {
			return fmt.Errorf("failed to download %s: %v", link, err)
		}
	}

	return nil
}

func findPDFLinks(ctx context.Context) ([]string, error) {
	var pdfLinks []string
	var hasMorePages = true
	var currentPage = 1

	// Loop through all pages
	for hasMorePages {
		log.Info("Processing document page", "page", currentPage)

		// Get the HTML content of the current page
		var html string
		if err := chromedp.Run(ctx,
			// Wait for the document list to appear
			chromedp.WaitVisible("#epaysliplist\\:ePayListForm\\:ePayslipDocs > div.ui-datatable-tablewrapper > table", chromedp.ByQuery),
			chromedp.OuterHTML("html", &html),
		); err != nil {
			return nil, fmt.Errorf("failed to get document page content: %v", err)
		}

		// Parse the HTML and find PDF links
		log.Debug("Parsing HTML for PDF links", "page", currentPage)
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML: %v", err)
		}

		// Find PDF links on current page
		var pageLinks []string
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			if href, exists := s.Attr("href"); exists && strings.Contains(href, "/AdpwAdpaWeb/DocDownload") {
				pageLinks = append(pageLinks, href)
			}
		})

		log.Info("Found PDF links on current page", "page", currentPage, "count", len(pageLinks))
		pdfLinks = append(pdfLinks, pageLinks...)

		// Check if there's a next page button that's not disabled
		var nextPageDisabled bool
		nextPageSelector := `a[aria-label="Nächste Seite"]`

		// First check if the next page button exists and is not disabled
		if err := chromedp.Run(ctx, chromedp.Evaluate(`
			(function() {
				const nextBtn = document.querySelector('a[aria-label="Nächste Seite"]');
				return !nextBtn || nextBtn.classList.contains('ui-state-disabled');
			})()
		`, &nextPageDisabled)); err != nil {
			return nil, fmt.Errorf("failed to check next page button: %v", err)
		}

		if nextPageDisabled {
			// No more pages
			hasMorePages = false
			log.Info("Reached last page", "total_pages", currentPage)
		} else {
			// Click next page button
			log.Info("Navigating to next page")
			if err := chromedp.Run(ctx,
				chromedp.Click(nextPageSelector, chromedp.ByQuery),
				// Wait for page to load
				chromedp.Sleep(2*time.Second),
				// Wait for the table to be visible again
				chromedp.WaitVisible("#epaysliplist\\:ePayListForm\\:ePayslipDocs > div.ui-datatable-tablewrapper > table", chromedp.ByQuery),
			); err != nil {
				return nil, fmt.Errorf("failed to navigate to next page: %v", err)
			}
			currentPage++
		}
	}

	log.Info("Total PDF links found across all pages", "count", len(pdfLinks))
	return pdfLinks, nil
}

func downloadFile(client *http.Client, urlStr, filepath string) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	log.Debug("Downloading file", "url", urlStr)
	resp, err := client.Get(urlStr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	log.Info("Successfully downloaded file", "path", filepath, "size_bytes", resp.ContentLength)
	return nil
}
