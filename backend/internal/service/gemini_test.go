package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"campusbite/internal/models"
	"campusbite/internal/service"
)

// TestHTTPGeminiService_EmptyKeyFallback tests that missing API key automatically uses heuristic classifier.
func TestHTTPGeminiService_EmptyKeyFallback(t *testing.T) {
	svc := service.NewHTTPGeminiService("", "gemini-2.5-flash")
	dto, err := svc.ClassifyIntent(context.Background(), "Top 5 selling items this week")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Intent != models.AnalyticsIntentTopSellingItems {
		t.Errorf("expected intent %s, got %s", models.AnalyticsIntentTopSellingItems, dto.Intent)
	}
	if dto.Period != models.AnalyticsPeriodThisWeek {
		t.Errorf("expected period %s, got %s", models.AnalyticsPeriodThisWeek, dto.Period)
	}
	if dto.Limit != 5 {
		t.Errorf("expected limit 5, got %d", dto.Limit)
	}
}

func TestFallbackHeuristicClassifier_TopSelling(t *testing.T) {
	dto, err := service.FallbackHeuristicClassifier("Top 5 selling items this week")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Intent != models.AnalyticsIntentTopSellingItems {
		t.Errorf("expected intent %s, got %s", models.AnalyticsIntentTopSellingItems, dto.Intent)
	}
	if dto.Period != models.AnalyticsPeriodThisWeek {
		t.Errorf("expected period %s, got %s", models.AnalyticsPeriodThisWeek, dto.Period)
	}
	if dto.Limit != 5 {
		t.Errorf("expected limit 5, got %d", dto.Limit)
	}
}

func TestFallbackHeuristicClassifier_AllIntents(t *testing.T) {
	tests := []struct {
		question       string
		expectedIntent string
		expectedPeriod string
		expectedLimit  int
	}{
		{
			question:       "Top 10 selling items today",
			expectedIntent: models.AnalyticsIntentTopSellingItems,
			expectedPeriod: models.AnalyticsPeriodToday,
			expectedLimit:  10,
		},
		{
			question:       "What were the best sellers yesterday?",
			expectedIntent: models.AnalyticsIntentTopSellingItems,
			expectedPeriod: models.AnalyticsPeriodYesterday,
			expectedLimit:  5,
		},
		{
			question:       "Show revenue summary this month",
			expectedIntent: models.AnalyticsIntentRevenueSummary,
			expectedPeriod: models.AnalyticsPeriodThisMonth,
			expectedLimit:  5,
		},
		{
			question:       "How many orders did we receive last week?",
			expectedIntent: models.AnalyticsIntentOrderCount,
			expectedPeriod: models.AnalyticsPeriodLastWeek,
			expectedLimit:  5,
		},
		{
			question:       "What is the average order value this week?",
			expectedIntent: models.AnalyticsIntentAverageOrderValue,
			expectedPeriod: models.AnalyticsPeriodThisWeek,
			expectedLimit:  5,
		},
		{
			question:       "Which category sold the most?",
			expectedIntent: models.AnalyticsIntentBestSellingCategory,
			expectedPeriod: models.AnalyticsPeriodThisWeek,
			expectedLimit:  5,
		},
		{
			question:       "Show daily sales breakdown for this week",
			expectedIntent: models.AnalyticsIntentSalesByDay,
			expectedPeriod: models.AnalyticsPeriodThisWeek,
			expectedLimit:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.question, func(t *testing.T) {
			dto, err := service.FallbackHeuristicClassifier(tt.question)
			if err != nil {
				t.Fatalf("failed to classify %q: %v", tt.question, err)
			}
			if dto.Intent != tt.expectedIntent {
				t.Errorf("question %q: expected intent %s, got %s", tt.question, tt.expectedIntent, dto.Intent)
			}
			if dto.Period != tt.expectedPeriod {
				t.Errorf("question %q: expected period %s, got %s", tt.question, tt.expectedPeriod, dto.Period)
			}
			if dto.Limit != tt.expectedLimit {
				t.Errorf("question %q: expected limit %d, got %d", tt.question, tt.expectedLimit, dto.Limit)
			}
		})
	}
}

func TestFallbackHeuristicClassifier_UnrecognizedQuery(t *testing.T) {
	// A completely nonsensical query with no analytics keywords should return ErrUnsupportedIntent
	_, err := service.FallbackHeuristicClassifier("what is the airspeed velocity of an unladen swallow")
	if !errors.Is(err, service.ErrUnsupportedIntent) {
		t.Errorf("expected ErrUnsupportedIntent for non-analytics question, got %v", err)
	}
}

func TestDeterministicExplanations(t *testing.T) {
	// Top selling
	items := []models.TopSellingItemResult{
		{ItemName: "Paneer Roll", UnitsSold: 50, TotalRevenue: 5000.00},
	}
	expTop := service.GenerateFallbackExplanation(models.AnalyticsIntentTopSellingItems, models.AnalyticsPeriodThisWeek, items)
	if !strings.Contains(expTop, "Paneer Roll") || !strings.Contains(expTop, "50") {
		t.Errorf("unexpected top selling explanation: %s", expTop)
	}

	// Revenue summary
	rev := &models.RevenueSummaryResult{
		TotalRevenue:      12500.50,
		TotalOrders:       100,
		AverageOrderValue: 125.00,
	}
	expRev := service.GenerateFallbackExplanation(models.AnalyticsIntentRevenueSummary, models.AnalyticsPeriodThisWeek, rev)
	if !strings.Contains(expRev, "12500.50") {
		t.Errorf("unexpected revenue explanation: %s", expRev)
	}
}
