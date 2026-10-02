package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"campusbite/internal/models"
)

var (
	ErrGeminiUnavailable   = errors.New("gemini analytics service unavailable")
	ErrInvalidGeminiOutput = errors.New("gemini returned invalid structured output")
)

// GeminiService defines the interface for AI-powered intent classification and result explanation.
type GeminiService interface {
	ClassifyIntent(ctx context.Context, question string) (*models.AnalyticsIntentDTO, error)
	GenerateExplanation(ctx context.Context, question string, intent string, period string, data interface{}) (string, error)
	GenerateTrendExplanation(ctx context.Context, trendData *models.TrendAnalyticsResponse) (string, error)
}

// HTTPGeminiService calls Google's Gemini API over HTTPS.
type HTTPGeminiService struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewHTTPGeminiService creates a new HTTPGeminiService.
func NewHTTPGeminiService(apiKey, model string) *HTTPGeminiService {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &HTTPGeminiService{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
	Temperature      float64 `json:"temperature,omitempty"`
}

type geminiRequestBody struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiResponseBody struct {
	Candidates []geminiCandidate `json:"candidates"`
	Error      *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// ClassifyIntent uses Gemini to parse a natural language question into a structured intent and period.
func (s *HTTPGeminiService) ClassifyIntent(ctx context.Context, question string) (*models.AnalyticsIntentDTO, error) {
	if s.apiKey == "" {
		// Fallback to local heuristic classifier if no API key is provided
		return FallbackHeuristicClassifier(question)
	}

	systemPrompt := `You are an analytics query classifier for CampusBite cafeteria management.
Your ONLY job is to extract the intent, time period, and optional limit from the user's business question.
You MUST output valid JSON ONLY with this exact schema:
{
  "intent": "TOP_SELLING_ITEMS" | "REVENUE_SUMMARY" | "ORDER_COUNT" | "AVERAGE_ORDER_VALUE" | "BEST_SELLING_CATEGORY" | "SALES_BY_DAY",
  "period": "TODAY" | "YESTERDAY" | "THIS_WEEK" | "LAST_WEEK" | "THIS_MONTH" | "LAST_MONTH",
  "limit": integer between 1 and 20 (default 5 for top/ranking queries)
}

Rules:
- Default period to "THIS_WEEK" if no timeframe is specified.
- Never output SQL, never output explanations, never output markdown outside JSON.`

	reqBody := geminiRequestBody{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: question}},
			},
		},
		GenerationConfig: &geminiGenerationConfig{
			ResponseMimeType: "application/json",
			Temperature:      0.1,
		},
	}

	rawText, err := s.callGeminiAPI(ctx, reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGeminiUnavailable, err)
	}

	// Sanitize output (strip potential markdown backticks)
	cleanJSON := sanitizeJSONResponse(rawText)

	var result models.AnalyticsIntentDTO
	if err := json.Unmarshal([]byte(cleanJSON), &result); err != nil {
		return nil, fmt.Errorf("%w: failed to parse JSON: %v", ErrInvalidGeminiOutput, err)
	}

	result.Intent = strings.ToUpper(strings.TrimSpace(result.Intent))
	result.Period = strings.ToUpper(strings.TrimSpace(result.Period))
	if result.Limit <= 0 {
		result.Limit = 5
	}
	if result.Limit > 20 {
		result.Limit = 20
	}

	return &result, nil
}

// GenerateExplanation asks Gemini to generate a concise summary based strictly on the authoritative DB data.
func (s *HTTPGeminiService) GenerateExplanation(ctx context.Context, question string, intent string, period string, data interface{}) (string, error) {
	if s.apiKey == "" {
		return GenerateFallbackExplanation(intent, period, data), nil
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		dataJSON = []byte("{}")
	}

	systemPrompt := `You are a helpful business analytics assistant for CampusBite cafeteria.
Given the user's question, intent, time period, and authoritative data computed by PostgreSQL, write a concise, professional 1-2 sentence summary.
Rules:
- Ground your answer STRICTLY in the provided data.
- NEVER invent or alter numerical values.
- Return plain text only.`

	prompt := fmt.Sprintf("User Question: %s\nIntent: %s\nPeriod: %s\nComputed Data:\n%s", question, intent, period, string(dataJSON))

	reqBody := geminiRequestBody{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: prompt}},
			},
		},
		GenerationConfig: &geminiGenerationConfig{
			Temperature: 0.2,
		},
	}

	explanation, err := s.callGeminiAPI(ctx, reqBody)
	if err != nil {
		// Non-fatal: return fallback explanation
		return GenerateFallbackExplanation(intent, period, data), nil
	}

	return strings.TrimSpace(explanation), nil
}

func (s *HTTPGeminiService) callGeminiAPI(ctx context.Context, reqBody geminiRequestBody) (string, error) {
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", s.model, s.apiKey)

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("gemini api http request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read gemini api response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini api returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var geminiResp geminiResponseBody
	if err := json.Unmarshal(bodyBytes, &geminiResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal gemini response: %w", err)
	}

	if geminiResp.Error != nil {
		return "", fmt.Errorf("gemini api error: %s (code %d)", geminiResp.Error.Message, geminiResp.Error.Code)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("empty response candidates from gemini")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

func sanitizeJSONResponse(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 2 {
			// drop first line (```json) and last line (```)
			if strings.HasPrefix(lines[len(lines)-1], "```") {
				lines = lines[1 : len(lines)-1]
			} else {
				lines = lines[1:]
			}
			trimmed = strings.Join(lines, "\n")
		}
	}
	return strings.TrimSpace(trimmed)
}

// FallbackHeuristicClassifier provides a zero-dependency intent classifier when no Gemini API key is configured.
func FallbackHeuristicClassifier(question string) (*models.AnalyticsIntentDTO, error) {
	q := strings.ToLower(question)

	// Period detection
	period := models.AnalyticsPeriodThisWeek
	if strings.Contains(q, "today") {
		period = models.AnalyticsPeriodToday
	} else if strings.Contains(q, "yesterday") {
		period = models.AnalyticsPeriodYesterday
	} else if strings.Contains(q, "last week") {
		period = models.AnalyticsPeriodLastWeek
	} else if strings.Contains(q, "this week") {
		period = models.AnalyticsPeriodThisWeek
	} else if strings.Contains(q, "last month") {
		period = models.AnalyticsPeriodLastMonth
	} else if strings.Contains(q, "this month") || strings.Contains(q, "month") {
		period = models.AnalyticsPeriodThisMonth
	}

	// Limit extraction (e.g. top 5, top 10)
	limit := 5
	re := regexp.MustCompile(`top\s+(\d+)`)
	if matches := re.FindStringSubmatch(q); len(matches) > 1 {
		if val, err := strconv.Atoi(matches[1]); err == nil && val > 0 && val <= 20 {
			limit = val
		}
	}

	// Intent detection
	var intent string
	if strings.Contains(q, "top") || strings.Contains(q, "best selling item") || strings.Contains(q, "selling item") || strings.Contains(q, "popular item") {
		intent = models.AnalyticsIntentTopSellingItems
	} else if strings.Contains(q, "category") || strings.Contains(q, "categories") {
		intent = models.AnalyticsIntentBestSellingCategory
	} else if strings.Contains(q, "average order") || strings.Contains(q, "aov") {
		intent = models.AnalyticsIntentAverageOrderValue
	} else if strings.Contains(q, "revenue") || strings.Contains(q, "earned") || strings.Contains(q, "sales amount") || strings.Contains(q, "how much did we make") {
		intent = models.AnalyticsIntentRevenueSummary
	} else if strings.Contains(q, "by day") || strings.Contains(q, "daily") || strings.Contains(q, "each day") || strings.Contains(q, "day by day") {
		intent = models.AnalyticsIntentSalesByDay
	} else if strings.Contains(q, "order") || strings.Contains(q, "how many orders") || strings.Contains(q, "count") {
		intent = models.AnalyticsIntentOrderCount
	} else {
		intent = models.AnalyticsIntentRevenueSummary
	}

	return &models.AnalyticsIntentDTO{
		Intent: intent,
		Period: period,
		Limit:  limit,
	}, nil
}

// GenerateFallbackExplanation creates deterministic plain text summaries from PostgreSQL data.
func GenerateFallbackExplanation(intent, period string, data interface{}) string {
	switch intent {
	case models.AnalyticsIntentTopSellingItems:
		if items, ok := data.([]models.TopSellingItemResult); ok && len(items) > 0 {
			return fmt.Sprintf("For %s, your top-selling item was '%s' with %d units sold generating ₹%.2f in revenue.",
				formatPeriodText(period), items[0].ItemName, items[0].UnitsSold, items[0].TotalRevenue)
		}
		return fmt.Sprintf("No top selling items recorded for %s.", formatPeriodText(period))

	case models.AnalyticsIntentRevenueSummary:
		if rev, ok := data.(*models.RevenueSummaryResult); ok && rev != nil {
			return fmt.Sprintf("Total revenue for %s is ₹%.2f across %d completed orders (Average Order Value: ₹%.2f).",
				formatPeriodText(period), rev.TotalRevenue, rev.TotalOrders, rev.AverageOrderValue)
		}

	case models.AnalyticsIntentOrderCount:
		if counts, ok := data.(*models.OrderCountResult); ok && counts != nil {
			return fmt.Sprintf("Total orders for %s: %d (%d completed, %d in progress, %d cancelled).",
				formatPeriodText(period), counts.TotalOrders, counts.CompletedOrders, counts.InProgressOrders, counts.CancelledOrders)
		}

	case models.AnalyticsIntentAverageOrderValue:
		if aov, ok := data.(*models.AverageOrderValueResult); ok && aov != nil {
			return fmt.Sprintf("The average order value for %s was ₹%.2f across %d qualifying orders.",
				formatPeriodText(period), aov.AverageOrderValue, aov.TotalQualifyingOrders)
		}

	case models.AnalyticsIntentBestSellingCategory:
		if cats, ok := data.([]models.BestSellingCategoryResult); ok && len(cats) > 0 {
			return fmt.Sprintf("For %s, the top catalog item was '%s' with %d units sold.",
				formatPeriodText(period), cats[0].CategoryOrItem, cats[0].UnitsSold)
		}
		return fmt.Sprintf("No category sales data found for %s.", formatPeriodText(period))

	case models.AnalyticsIntentSalesByDay:
		if days, ok := data.([]models.SalesByDayResult); ok {
			return fmt.Sprintf("Sales breakdown across %d recorded days for %s.", len(days), formatPeriodText(period))
		}
	}

	return fmt.Sprintf("Analytics results computed successfully for %s.", formatPeriodText(period))
}

func formatPeriodText(p string) string {
	switch strings.ToUpper(p) {
	case models.AnalyticsPeriodToday:
		return "today"
	case models.AnalyticsPeriodYesterday:
		return "yesterday"
	case models.AnalyticsPeriodThisWeek:
		return "this week"
	case models.AnalyticsPeriodLastWeek:
		return "last week"
	case models.AnalyticsPeriodThisMonth:
		return "this month"
	case models.AnalyticsPeriodLastMonth:
		return "last month"
	default:
		return p
	}
}

// GenerateTrendExplanation asks Gemini to provide a business insight summary for computed trend data.
func (s *HTTPGeminiService) GenerateTrendExplanation(ctx context.Context, trendData *models.TrendAnalyticsResponse) (string, error) {
	if s.apiKey == "" {
		return GenerateFallbackTrendExplanation(trendData), nil
	}

	dataJSON, err := json.Marshal(trendData)
	if err != nil {
		dataJSON = []byte("{}")
	}

	systemPrompt := `You are a helpful business analytics assistant for CampusBite cafeteria.
Given the period-over-period trend insights and metrics computed by PostgreSQL and the backend, write a concise, professional 1-3 sentence summary explaining the key sales trends (e.g., strongest growth items, declining items, and overall period performance).
Rules:
- Ground your explanation STRICTLY in the provided data.
- NEVER invent, alter, or recalculate numerical values.
- Return plain text only.`

	prompt := fmt.Sprintf("Period: %s (vs %s)\nComputed Trend Data:\n%s", trendData.Period, trendData.ComparisonPeriod, string(dataJSON))

	reqBody := geminiRequestBody{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: prompt}},
			},
		},
		GenerationConfig: &geminiGenerationConfig{
			Temperature: 0.2,
		},
	}

	explanation, err := s.callGeminiAPI(ctx, reqBody)
	if err != nil {
		// Non-fatal: return fallback explanation
		return GenerateFallbackTrendExplanation(trendData), nil
	}

	return strings.TrimSpace(explanation), nil
}

// GenerateFallbackTrendExplanation creates deterministic plain text trend summaries from computed data.
func GenerateFallbackTrendExplanation(trendData *models.TrendAnalyticsResponse) string {
	if trendData == nil || len(trendData.Insights) == 0 {
		return fmt.Sprintf("No historical sales trend data available for %s compared to %s.",
			formatPeriodText(trendData.Period), formatPeriodText(trendData.ComparisonPeriod))
	}

	var increasingItems []string
	var decreasingItems []string
	var newItems []string

	for _, item := range trendData.Insights {
		switch item.Trend {
		case models.TrendIncreasing:
			if item.GrowthPercent != nil {
				increasingItems = append(increasingItems, fmt.Sprintf("%s (+%.1f%%)", item.ItemName, *item.GrowthPercent))
			} else {
				increasingItems = append(increasingItems, item.ItemName)
			}
		case models.TrendDecreasing:
			if item.GrowthPercent != nil {
				decreasingItems = append(decreasingItems, fmt.Sprintf("%s (%.1f%%)", item.ItemName, *item.GrowthPercent))
			} else {
				decreasingItems = append(decreasingItems, item.ItemName)
			}
		case models.TrendNew:
			newItems = append(newItems, item.ItemName)
		}
	}

	var parts []string
	if len(increasingItems) > 0 {
		parts = append(parts, fmt.Sprintf("Strong growth observed in %s", strings.Join(increasingItems, ", ")))
	}
	if len(newItems) > 0 {
		parts = append(parts, fmt.Sprintf("New demand recorded for %s", strings.Join(newItems, ", ")))
	}
	if len(decreasingItems) > 0 {
		parts = append(parts, fmt.Sprintf("Declining sales noted in %s", strings.Join(decreasingItems, ", ")))
	}

	summaryText := ""
	if trendData.Summary.OverallGrowthPercent != nil {
		summaryText = fmt.Sprintf("Overall volume grew by %.1f%% compared to %s.", *trendData.Summary.OverallGrowthPercent, formatPeriodText(trendData.ComparisonPeriod))
	} else {
		summaryText = fmt.Sprintf("Overall trend is %s compared to %s.", trendData.Summary.OverallTrend, formatPeriodText(trendData.ComparisonPeriod))
	}

	if len(parts) > 0 {
		return fmt.Sprintf("%s. %s", strings.Join(parts, "; "), summaryText)
	}

	return fmt.Sprintf("Sales remained stable across items compared to %s. %s", formatPeriodText(trendData.ComparisonPeriod), summaryText)
}

// MockGeminiService is used in tests to simulate arbitrary or controlled Gemini responses.
type MockGeminiService struct {
	ClassifyFunc         func(ctx context.Context, question string) (*models.AnalyticsIntentDTO, error)
	ExplanationFunc      func(ctx context.Context, question string, intent string, period string, data interface{}) (string, error)
	TrendExplanationFunc func(ctx context.Context, trendData *models.TrendAnalyticsResponse) (string, error)
}

func (m *MockGeminiService) ClassifyIntent(ctx context.Context, question string) (*models.AnalyticsIntentDTO, error) {
	if m.ClassifyFunc != nil {
		return m.ClassifyFunc(ctx, question)
	}
	return FallbackHeuristicClassifier(question)
}

func (m *MockGeminiService) GenerateExplanation(ctx context.Context, question string, intent string, period string, data interface{}) (string, error) {
	if m.ExplanationFunc != nil {
		return m.ExplanationFunc(ctx, question, intent, period, data)
	}
	return GenerateFallbackExplanation(intent, period, data), nil
}

func (m *MockGeminiService) GenerateTrendExplanation(ctx context.Context, trendData *models.TrendAnalyticsResponse) (string, error) {
	if m.TrendExplanationFunc != nil {
		return m.TrendExplanationFunc(ctx, trendData)
	}
	return GenerateFallbackTrendExplanation(trendData), nil
}
