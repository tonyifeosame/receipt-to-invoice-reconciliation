package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"receipt-reconciliation/models"
	"time"
)

type CompanyAPIClient struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
	maxRetries int
	retryDelay time.Duration
}

type CompanyAPIResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	Timestamp string `json:"timestamp"`
}

func NewCompanyAPIClient() *CompanyAPIClient {
	baseURL := os.Getenv("COMPANY_API_URL")
	if baseURL == "" {
		baseURL = "https://api.company.example.com"
	}

	apiKey := os.Getenv("COMPANY_API_KEY")
	if apiKey == "" {
		log.Println("Warning: COMPANY_API_KEY not set, using empty key")
	}

	maxRetries := 3
	retryDelay := 5 * time.Second

	return &CompanyAPIClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiKey:     apiKey,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}
}

func (c *CompanyAPIClient) SendOCRResults(ocrData models.OCRResponse) (*CompanyAPIResponse, error) {
	endpoint := fmt.Sprintf("%s/api/receipts/ocr", c.baseURL)

	jsonData, err := json.Marshal(ocrData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OCR data: %w", err)
	}

	log.Printf("Sending OCR results to company API: %s (request_id: %s)", endpoint, ocrData.RequestID)

	var lastError error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retry attempt %d/%d for request %s", attempt, c.maxRetries, ocrData.RequestID)
			time.Sleep(c.retryDelay)
		}

		req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
		if err != nil {
			lastError = fmt.Errorf("failed to create request: %w", err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("X-API-Key", c.apiKey)
		req.Header.Set("X-Request-ID", ocrData.RequestID)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastError = fmt.Errorf("failed to send request to company API: %w", err)
			log.Printf("Request failed (attempt %d): %v", attempt+1, err)
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastError = fmt.Errorf("failed to read response body: %w", err)
			continue
		}

		// Handle different HTTP status codes
		switch resp.StatusCode {
		case http.StatusOK:
			var apiResponse CompanyAPIResponse
			if err := json.Unmarshal(body, &apiResponse); err != nil {
				return nil, fmt.Errorf("failed to parse company API response: %w", err)
			}
			log.Printf("Company API response: success=%v, message=%s", apiResponse.Success, apiResponse.Message)
			return &apiResponse, nil
		case http.StatusBadRequest:
			return &CompanyAPIResponse{
				Success:   false,
				Message:   fmt.Sprintf("Validation error: %s", string(body)),
				Timestamp: time.Now().Format(time.RFC3339),
			}, fmt.Errorf("company API returned validation error (400): %s", string(body))
		case http.StatusUnauthorized:
			return &CompanyAPIResponse{
				Success:   false,
				Message:   "Authentication failed - invalid API credentials",
				Timestamp: time.Now().Format(time.RFC3339),
			}, fmt.Errorf("company API returned unauthorized (401): check API credentials")
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
			lastError = fmt.Errorf("company API returned server error (%d): %s", resp.StatusCode, string(body))
			log.Printf("Server error (attempt %d): %v", attempt+1, lastError)
			continue // Retry on server errors
		default:
			return &CompanyAPIResponse{
				Success:   false,
				Message:   fmt.Sprintf("Unexpected error: %s", string(body)),
				Timestamp: time.Now().Format(time.RFC3339),
			}, fmt.Errorf("company API returned unexpected status %d: %s", resp.StatusCode, string(body))
		}
	}

	// All retries exhausted
	return &CompanyAPIResponse{
		Success:   false,
		Message:   fmt.Sprintf("Failed after %d retries: %v", c.maxRetries, lastError),
		Timestamp: time.Now().Format(time.RFC3339),
	}, fmt.Errorf("failed to send OCR results after %d retries: %w", c.maxRetries, lastError)
}

func (c *CompanyAPIClient) GetReceiptStatus(receiptFile string) (*CompanyAPIResponse, error) {
	endpoint := fmt.Sprintf("%s/api/receipts/status?file=%s", c.baseURL, receiptFile)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to company API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("company API returned error status %d: %s", resp.StatusCode, string(body))
	}

	var apiResponse CompanyAPIResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse company API response: %w", err)
	}

	return &apiResponse, nil
}
