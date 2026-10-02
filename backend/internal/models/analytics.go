package models

import (
	"fmt"
	"strings"
	"time"
)

// Supported Analytics Intents (Allowlist)
const (
	AnalyticsIntentTopSellingItems     = "TOP_SELLING_ITEMS"
	AnalyticsIntentRevenueSummary      = "REVENUE_SUMMARY"
	AnalyticsIntentOrderCount          = "ORDER_COUNT"
	AnalyticsIntentAverageOrderValue   = "AVERAGE_ORDER_VALUE"
	AnalyticsIntentBestSellingCategory = "BEST_SELLING_CATEGORY"
	AnalyticsIntentSalesByDay          = "SALES_BY_DAY"
)

// Supported Analytics Periods (Allowlist)
const (
	AnalyticsPeriodToday     = "TODAY"
	AnalyticsPeriodYesterday = "YESTERDAY"
	AnalyticsPeriodThisWeek  = "THIS_WEEK"
	AnalyticsPeriodLastWeek  = "LAST_WEEK"
	AnalyticsPeriodThisMonth = "THIS_MONTH"
	AnalyticsPeriodLastMonth = "LAST_MONTH"
)

// AllowedIntentsMap is a lookup map for validating intents.
var AllowedIntentsMap = map[string]bool{
	AnalyticsIntentTopSellingItems:     true,
	AnalyticsIntentRevenueSummary:      true,
	AnalyticsIntentOrderCount:          true,
	AnalyticsIntentAverageOrderValue:   true,
	AnalyticsIntentBestSellingCategory: true,
	AnalyticsIntentSalesByDay:          true,
}

// AllowedPeriodsMap is a lookup map for validating time periods.
var AllowedPeriodsMap = map[string]bool{
	AnalyticsPeriodToday:     true,
	AnalyticsPeriodYesterday: true,
	AnalyticsPeriodThisWeek:  true,
	AnalyticsPeriodLastWeek:  true,
	AnalyticsPeriodThisMonth: true,
	AnalyticsPeriodLastMonth: true,
}

// AnalyticsQueryRequest represents the incoming user question.
type AnalyticsQueryRequest struct {
	Question string `json:"question" binding:"required"`
}

// AnalyticsIntentDTO is the structured intent extracted by Gemini.
type AnalyticsIntentDTO struct {
	Intent string `json:"intent"`
	Period string `json:"period"`
	Limit  int    `json:"limit"`
}

// AnalyticsResponse is the authoritative API response returned to the client.
type AnalyticsResponse struct {
	Intent      string      `json:"intent"`
	Period      string      `json:"period"`
	Data        interface{} `json:"data"`
	Explanation string      `json:"explanation,omitempty"`
}

// Analytics Data Result Models

type TopSellingItemResult struct {
	ItemName     string  `json:"item_name"`
	UnitsSold    int64   `json:"units_sold"`
	TotalRevenue float64 `json:"total_revenue"`
}

type RevenueSummaryResult struct {
	TotalOrders       int64   `json:"total_orders"`
	TotalRevenue      float64 `json:"total_revenue"`
	AverageOrderValue float64 `json:"average_order_value"`
}

type OrderCountResult struct {
	TotalOrders      int64 `json:"total_orders"`
	CompletedOrders  int64 `json:"completed_orders"`
	InProgressOrders int64 `json:"in_progress_orders"`
	CancelledOrders  int64 `json:"cancelled_orders"`
}

type AverageOrderValueResult struct {
	AverageOrderValue     float64 `json:"average_order_value"`
	MinOrderValue         float64 `json:"min_order_value"`
	MaxOrderValue         float64 `json:"max_order_value"`
	TotalQualifyingOrders int64   `json:"total_qualifying_orders"`
}

type BestSellingCategoryResult struct {
	CategoryOrItem string  `json:"category_or_item"`
	UnitsSold      int64   `json:"units_sold"`
	TotalRevenue   float64 `json:"total_revenue"`
}

type SalesByDayResult struct {
	Day          string  `json:"day"`
	OrdersCount  int64   `json:"orders_count"`
	DailyRevenue float64 `json:"daily_revenue"`
}

// ResolvePeriodTimestamps calculates authoritative [startTime, endTime) UTC bounds for a given period.
func ResolvePeriodTimestamps(period string, refTime ...time.Time) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	if len(refTime) > 0 {
		now = refTime[0].UTC()
	}

	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	switch strings.ToUpper(strings.TrimSpace(period)) {
	case AnalyticsPeriodToday:
		return startOfToday, startOfToday.AddDate(0, 0, 1), nil

	case AnalyticsPeriodYesterday:
		startOfYesterday := startOfToday.AddDate(0, 0, -1)
		return startOfYesterday, startOfToday, nil

	case AnalyticsPeriodThisWeek:
		weekday := int(startOfToday.Weekday())
		if weekday == 0 { // Sunday is treated as day 7 in ISO weeks
			weekday = 7
		}
		startOfWeek := startOfToday.AddDate(0, 0, -(weekday - 1))
		return startOfWeek, startOfWeek.AddDate(0, 0, 7), nil

	case AnalyticsPeriodLastWeek:
		weekday := int(startOfToday.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		startOfThisWeek := startOfToday.AddDate(0, 0, -(weekday - 1))
		startOfLastWeek := startOfThisWeek.AddDate(0, 0, -7)
		return startOfLastWeek, startOfThisWeek, nil

	case AnalyticsPeriodThisMonth:
		startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return startOfMonth, startOfMonth.AddDate(0, 1, 0), nil

	case AnalyticsPeriodLastMonth:
		startOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		startOfLastMonth := startOfThisMonth.AddDate(0, -1, 0)
		return startOfLastMonth, startOfThisMonth, nil

	default:
		return time.Time{}, time.Time{}, fmt.Errorf("unsupported period: %s", period)
	}
}

// Supported Trend Classification Constants
const (
	TrendIncreasing = "INCREASING"
	TrendDecreasing = "DECREASING"
	TrendStable     = "STABLE"
	TrendNew        = "NEW"
)

// Supported Trend Periods
const (
	TrendPeriodToday     = "TODAY"
	TrendPeriodThisWeek  = "THIS_WEEK"
	TrendPeriodThisMonth = "THIS_MONTH"
)

// AllowedTrendPeriodsMap is an allowlist for validating trend analysis time periods.
var AllowedTrendPeriodsMap = map[string]bool{
	TrendPeriodToday:     true,
	TrendPeriodThisWeek:  true,
	TrendPeriodThisMonth: true,
}

// TrendAnalyticsRequest represents the request for trend insights.
type TrendAnalyticsRequest struct {
	Period string `json:"period"`
}

// ItemTrendInsight represents the computed comparative metrics for a single menu item.
type ItemTrendInsight struct {
	ItemID          string   `json:"item_id"`
	ItemName        string   `json:"item_name"`
	CurrentUnits    int64    `json:"current_units"`
	PreviousUnits   int64    `json:"previous_units"`
	CurrentRevenue  float64  `json:"current_revenue"`
	PreviousRevenue float64  `json:"previous_revenue"`
	GrowthPercent   *float64 `json:"growth_percent"` // null if previous_units == 0 and current_units > 0
	Trend           string   `json:"trend"`          // INCREASING, DECREASING, STABLE, NEW
}

// TrendSummary represents aggregate comparison metrics across all items.
type TrendSummary struct {
	TotalCurrentUnits    int64    `json:"total_current_units"`
	TotalPreviousUnits   int64    `json:"total_previous_units"`
	TotalCurrentRevenue  float64  `json:"total_current_revenue"`
	TotalPreviousRevenue float64  `json:"total_previous_revenue"`
	OverallGrowthPercent *float64 `json:"overall_growth_percent"`
	OverallTrend         string   `json:"overall_trend"`
}

// TrendAnalyticsResponse is the structured response returned by POST /api/v1/analytics/trends.
type TrendAnalyticsResponse struct {
	Period           string             `json:"period"`
	ComparisonPeriod string             `json:"comparison_period"`
	CurrentStart     time.Time          `json:"current_start"`
	CurrentEnd       time.Time          `json:"current_end"`
	PreviousStart    time.Time          `json:"previous_start"`
	PreviousEnd      time.Time          `json:"previous_end"`
	Insights         []ItemTrendInsight `json:"insights"`
	Summary          TrendSummary       `json:"summary"`
	Explanation      string             `json:"explanation,omitempty"`
}

// ItemTrendRawDBResult is the raw scanned row from the DB aggregation query.
type ItemTrendRawDBResult struct {
	ItemID          string  `json:"item_id"`
	ItemName        string  `json:"item_name"`
	CurrentUnits    int64   `json:"current_units"`
	PreviousUnits   int64   `json:"previous_units"`
	CurrentRevenue  float64 `json:"current_revenue"`
	PreviousRevenue float64 `json:"previous_revenue"`
}

// ResolveTrendPeriodTimestamps calculates authoritative current and previous UTC bounds.
func ResolveTrendPeriodTimestamps(period string, refTime ...time.Time) (currentStart, currentEnd, prevStart, prevEnd time.Time, comparisonPeriod string, err error) {
	now := time.Now().UTC()
	if len(refTime) > 0 {
		now = refTime[0].UTC()
	}

	normPeriod := strings.ToUpper(strings.TrimSpace(period))
	if normPeriod == "" {
		normPeriod = TrendPeriodThisWeek
	}

	switch normPeriod {
	case TrendPeriodToday:
		startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		currentStart = startOfToday
		currentEnd = startOfToday.AddDate(0, 0, 1)
		prevStart = startOfToday.AddDate(0, 0, -1)
		prevEnd = startOfToday
		comparisonPeriod = AnalyticsPeriodYesterday
		return currentStart, currentEnd, prevStart, prevEnd, comparisonPeriod, nil

	case TrendPeriodThisWeek:
		startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		weekday := int(startOfToday.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		startOfWeek := startOfToday.AddDate(0, 0, -(weekday - 1))
		currentStart = startOfWeek
		currentEnd = startOfWeek.AddDate(0, 0, 7)
		prevStart = startOfWeek.AddDate(0, 0, -7)
		prevEnd = startOfWeek
		comparisonPeriod = AnalyticsPeriodLastWeek
		return currentStart, currentEnd, prevStart, prevEnd, comparisonPeriod, nil

	case TrendPeriodThisMonth:
		startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		currentStart = startOfMonth
		currentEnd = startOfMonth.AddDate(0, 1, 0)
		prevStart = startOfMonth.AddDate(0, -1, 0)
		prevEnd = startOfMonth
		comparisonPeriod = AnalyticsPeriodLastMonth
		return currentStart, currentEnd, prevStart, prevEnd, comparisonPeriod, nil

	default:
		return time.Time{}, time.Time{}, time.Time{}, time.Time{}, "", fmt.Errorf("unsupported trend period: %s", period)
	}
}

// CalculateGrowthAndTrend deterministically computes growth percentage and trend classification.
// Handled cases:
// 1. previous == 0 and current > 0 -> growth: nil, trend: "NEW"
// 2. previous == 0 and current == 0 -> growth: 0.0, trend: "STABLE"
// 3. previous > 0 and current == 0 -> growth: -100.0, trend: "DECREASING"
// 4. previous > 0 and current > 0:
//   - growth > +10.0% -> "INCREASING"
//   - growth < -10.0% -> "DECREASING"
//   - otherwise       -> "STABLE"
func CalculateGrowthAndTrend(currentUnits, previousUnits int64) (*float64, string) {
	if previousUnits == 0 {
		if currentUnits > 0 {
			return nil, TrendNew
		}
		growth := 0.0
		return &growth, TrendStable
	}

	if currentUnits == 0 {
		growth := -100.0
		return &growth, TrendDecreasing
	}

	diff := float64(currentUnits) - float64(previousUnits)
	rawGrowth := (diff / float64(previousUnits)) * 100.0
	// Round to 2 decimal places
	roundedGrowth := float64(int64(rawGrowth*100+0.5*float64(sign(rawGrowth)))) / 100.0

	var trend string
	if roundedGrowth > 10.0 {
		trend = TrendIncreasing
	} else if roundedGrowth < -10.0 {
		trend = TrendDecreasing
	} else {
		trend = TrendStable
	}

	return &roundedGrowth, trend
}

func sign(f float64) int {
	if f < 0 {
		return -1
	}
	return 1
}
