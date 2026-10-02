package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"campusbite/internal/models"
	"campusbite/internal/repository"
)

var (
	ErrEmptyQuestion      = errors.New("analytics question cannot be empty")
	ErrQuestionTooLong    = errors.New("analytics question exceeds maximum length of 500 characters")
	ErrUnsupportedIntent  = errors.New("unsupported or unrecognized analytics intent")
	ErrUnsupportedPeriod  = errors.New("unsupported analytics time period")
	ErrAnalyticsExecution = errors.New("failed to execute analytics query")
)

// AnalyticsRouter coordinates natural-language understanding, backend validation, parameterized SQL execution, and summary explanation.
type AnalyticsRouter struct {
	geminiService GeminiService
	analyticsRepo *repository.AnalyticsRepository
}

// NewAnalyticsRouter creates a new AnalyticsRouter.
func NewAnalyticsRouter(geminiService GeminiService, analyticsRepo *repository.AnalyticsRepository) *AnalyticsRouter {
	return &AnalyticsRouter{
		geminiService: geminiService,
		analyticsRepo: analyticsRepo,
	}
}

// ExecuteQuery processes an incoming natural language business question securely.
// Invariant: Gemini selects the INTENT; the backend chooses and executes the predefined PARAMETERIZED SQL.
func (r *AnalyticsRouter) ExecuteQuery(ctx context.Context, question string) (*models.AnalyticsResponse, error) {
	cleanQuestion := strings.TrimSpace(question)
	if cleanQuestion == "" {
		return nil, ErrEmptyQuestion
	}
	if len(cleanQuestion) > 500 {
		return nil, ErrQuestionTooLong
	}

	start := time.Now()

	// 1. Convert natural-language question to structured intent using Gemini
	intentDTO, err := r.geminiService.ClassifyIntent(ctx, cleanQuestion)
	if err != nil {
		log.Printf("[AnalyticsRouter] Intent classification failed: %v", err)
		return nil, fmt.Errorf("%w: unable to understand analytics request", ErrUnsupportedIntent)
	}

	// 2. Strict Backend Validation (Security Boundary)
	intent := strings.ToUpper(strings.TrimSpace(intentDTO.Intent))
	if !models.AllowedIntentsMap[intent] {
		log.Printf("[AnalyticsRouter] Rejected untrusted/unknown intent from LLM: %s", intent)
		return nil, fmt.Errorf("%w: '%s'", ErrUnsupportedIntent, intent)
	}

	period := strings.ToUpper(strings.TrimSpace(intentDTO.Period))
	if !models.AllowedPeriodsMap[period] {
		log.Printf("[AnalyticsRouter] Rejected unsupported period: %s (defaulting to THIS_WEEK)", period)
		period = models.AnalyticsPeriodThisWeek
	}

	limit := intentDTO.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	// 3. Resolve authoritative UTC time bounds for period
	startTime, endTime, err := models.ResolvePeriodTimestamps(period)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedPeriod, err)
	}

	// 4. Dispatch to predefined parameterized SQL query
	var data interface{}
	switch intent {
	case models.AnalyticsIntentTopSellingItems:
		items, err := r.analyticsRepo.GetTopSellingItems(ctx, startTime, endTime, limit)
		if err != nil {
			log.Printf("[AnalyticsRouter] DB query error for TOP_SELLING_ITEMS: %v", err)
			return nil, ErrAnalyticsExecution
		}
		data = items

	case models.AnalyticsIntentRevenueSummary:
		rev, err := r.analyticsRepo.GetRevenueSummary(ctx, startTime, endTime)
		if err != nil {
			log.Printf("[AnalyticsRouter] DB query error for REVENUE_SUMMARY: %v", err)
			return nil, ErrAnalyticsExecution
		}
		data = rev

	case models.AnalyticsIntentOrderCount:
		counts, err := r.analyticsRepo.GetOrderCount(ctx, startTime, endTime)
		if err != nil {
			log.Printf("[AnalyticsRouter] DB query error for ORDER_COUNT: %v", err)
			return nil, ErrAnalyticsExecution
		}
		data = counts

	case models.AnalyticsIntentAverageOrderValue:
		aov, err := r.analyticsRepo.GetAverageOrderValue(ctx, startTime, endTime)
		if err != nil {
			log.Printf("[AnalyticsRouter] DB query error for AVERAGE_ORDER_VALUE: %v", err)
			return nil, ErrAnalyticsExecution
		}
		data = aov

	case models.AnalyticsIntentBestSellingCategory:
		cats, err := r.analyticsRepo.GetBestSellingCategories(ctx, startTime, endTime, limit)
		if err != nil {
			log.Printf("[AnalyticsRouter] DB query error for BEST_SELLING_CATEGORY: %v", err)
			return nil, ErrAnalyticsExecution
		}
		data = cats

	case models.AnalyticsIntentSalesByDay:
		days, err := r.analyticsRepo.GetSalesByDay(ctx, startTime, endTime)
		if err != nil {
			log.Printf("[AnalyticsRouter] DB query error for SALES_BY_DAY: %v", err)
			return nil, ErrAnalyticsExecution
		}
		data = days

	default:
		return nil, ErrUnsupportedIntent
	}

	// 5. Generate natural-language explanation using computed data (non-fatal)
	explanation, err := r.geminiService.GenerateExplanation(ctx, cleanQuestion, intent, period, data)
	if err != nil {
		log.Printf("[AnalyticsRouter] Warning: Explanation generation failed: %v", err)
		explanation = GenerateFallbackExplanation(intent, period, data)
	}

	duration := time.Since(start)
	log.Printf("[AnalyticsRouter] Executed intent=%s period=%s in %v", intent, period, duration)

	return &models.AnalyticsResponse{
		Intent:      intent,
		Period:      period,
		Data:        data,
		Explanation: explanation,
	}, nil
}

// ExecuteTrendAnalysis computes comparative historical period metrics and deterministic trend classifications.
// Invariants:
// 1. PostgreSQL calculates aggregate raw units and revenue per period via parameterized SQL.
// 2. Go backend performs all growth percentage and trend classification calculations.
// 3. Gemini is used only to provide natural-language explanations of already-computed insights.
// 4. If Gemini fails, the deterministic structured trend data is returned intact.
func (r *AnalyticsRouter) ExecuteTrendAnalysis(ctx context.Context, req models.TrendAnalyticsRequest) (*models.TrendAnalyticsResponse, error) {
	start := time.Now()

	period := strings.ToUpper(strings.TrimSpace(req.Period))
	if period == "" {
		period = models.TrendPeriodThisWeek
	}

	if !models.AllowedTrendPeriodsMap[period] {
		return nil, fmt.Errorf("%w: '%s'", ErrUnsupportedPeriod, period)
	}

	currStart, currEnd, prevStart, prevEnd, compPeriod, err := models.ResolveTrendPeriodTimestamps(period)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedPeriod, err)
	}

	rawResults, err := r.analyticsRepo.GetItemTrends(ctx, currStart, currEnd, prevStart, prevEnd)
	if err != nil {
		log.Printf("[AnalyticsRouter] DB query error for GetItemTrends: %v", err)
		return nil, ErrAnalyticsExecution
	}

	insights := make([]models.ItemTrendInsight, 0, len(rawResults))
	var totalCurrUnits, totalPrevUnits int64
	var totalCurrRev, totalPrevRev float64

	for _, raw := range rawResults {
		growth, trend := models.CalculateGrowthAndTrend(raw.CurrentUnits, raw.PreviousUnits)
		insights = append(insights, models.ItemTrendInsight{
			ItemID:          raw.ItemID,
			ItemName:        raw.ItemName,
			CurrentUnits:    raw.CurrentUnits,
			PreviousUnits:   raw.PreviousUnits,
			CurrentRevenue:  raw.CurrentRevenue,
			PreviousRevenue: raw.PreviousRevenue,
			GrowthPercent:   growth,
			Trend:           trend,
		})

		totalCurrUnits += raw.CurrentUnits
		totalPrevUnits += raw.PreviousUnits
		totalCurrRev += raw.CurrentRevenue
		totalPrevRev += raw.PreviousRevenue
	}

	overallGrowth, overallTrend := models.CalculateGrowthAndTrend(totalCurrUnits, totalPrevUnits)
	summary := models.TrendSummary{
		TotalCurrentUnits:    totalCurrUnits,
		TotalPreviousUnits:   totalPrevUnits,
		TotalCurrentRevenue:  float64(int64(totalCurrRev*100+0.5)) / 100.0,
		TotalPreviousRevenue: float64(int64(totalPrevRev*100+0.5)) / 100.0,
		OverallGrowthPercent: overallGrowth,
		OverallTrend:         overallTrend,
	}

	resp := &models.TrendAnalyticsResponse{
		Period:           period,
		ComparisonPeriod: compPeriod,
		CurrentStart:     currStart,
		CurrentEnd:       currEnd,
		PreviousStart:    prevStart,
		PreviousEnd:      prevEnd,
		Insights:         insights,
		Summary:          summary,
	}

	// Generate natural-language explanation using computed data (non-fatal)
	explanation, err := r.geminiService.GenerateTrendExplanation(ctx, resp)
	if err != nil {
		log.Printf("[AnalyticsRouter] Warning: Trend explanation generation failed: %v", err)
		explanation = GenerateFallbackTrendExplanation(resp)
	}
	resp.Explanation = explanation

	duration := time.Since(start)
	log.Printf("[AnalyticsRouter] Executed TrendAnalysis period=%s (vs %s) items=%d in %v", period, compPeriod, len(insights), duration)

	return resp, nil
}
