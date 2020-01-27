package usagestats

import (
	"context"

	"github.com/sourcegraph/sourcegraph/cmd/frontend/db"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/types"
)

// CodeIntelUsageStatisticsOptions contains options for the number of daily, weekly, and monthly
// periods in which to calculate the number of events and latency percentiles.
type CodeIntelUsageStatisticsOptions struct {
	DayPeriods            *int
	WeekPeriods           *int
	MonthPeriods          *int
	IncludeEventCounts    bool
	IncludeEventLatencies bool
}

type (
	usagePeriod     = types.CodeIntelUsagePeriod
	eventStatistics = types.CodeIntelEventStatistics
	eventLatencies  = types.CodeIntelEventLatencies
)

var (
	DurationField       = "durationMs"
	DurationPercentiles = []float64{0.5, 0.9, 0.99}
)

// GetCodeIntelUsageStatistics returns the current site's code intel activity.
func GetCodeIntelUsageStatistics(ctx context.Context, opt *CodeIntelUsageStatisticsOptions) (*types.CodeIntelUsageStatistics, error) {
	var (
		dayPeriods   = defaultDays
		weekPeriods  = defaultWeeks
		monthPeriods = defaultMonths
	)

	if opt != nil {
		if opt.DayPeriods != nil {
			dayPeriods = minIntOrZero(maxStorageDays, *opt.DayPeriods)
		}
		if opt.WeekPeriods != nil {
			weekPeriods = minIntOrZero(maxStorageDays/7, *opt.WeekPeriods)
		}
		if opt.MonthPeriods != nil {
			monthPeriods = minIntOrZero(maxStorageDays/31, *opt.MonthPeriods)
		}
	}

	dailyActivities, err := codeIntelActivity(ctx, db.Daily, dayPeriods, opt.IncludeEventCounts, opt.IncludeEventLatencies)
	if err != nil {
		return nil, err
	}
	weeklyActivities, err := codeIntelActivity(ctx, db.Weekly, weekPeriods, opt.IncludeEventCounts, opt.IncludeEventLatencies)
	if err != nil {
		return nil, err
	}
	monthlyActivities, err := codeIntelActivity(ctx, db.Monthly, monthPeriods, opt.IncludeEventCounts, opt.IncludeEventLatencies)
	if err != nil {
		return nil, err
	}
	return &types.CodeIntelUsageStatistics{
		DailyActivities:   dailyActivities,
		WeeklyActivities:  weeklyActivities,
		MonthlyActivities: monthlyActivities,
	}, nil
}

func codeIntelActivity(ctx context.Context, periodType db.PeriodType, periods int, includeEventCounts, includeEventLatencies bool) ([]*types.CodeIntelUsagePeriod, error) {
	if periods == 0 {
		return []*types.CodeIntelUsagePeriod{}, nil
	}

	periods = periods - 1
	startDate, err := startOfPeriod(periodType, periods)
	if err != nil {
		return nil, err
	}

	activityPeriods := []*types.CodeIntelUsagePeriod{}
	for i := 0; i <= periods; i++ {
		activityPeriods = append(activityPeriods, &usagePeriod{
			PreciseHoverStatistics:       &eventStatistics{EventLatencies: &eventLatencies{}},
			FuzzyHoverStatistics:         &eventStatistics{EventLatencies: &eventLatencies{}},
			PreciseDefinitionsStatistics: &eventStatistics{EventLatencies: &eventLatencies{}},
			FuzzyDefinitionsStatistics:   &eventStatistics{EventLatencies: &eventLatencies{}},
			PreciseReferencesStatistics:  &eventStatistics{EventLatencies: &eventLatencies{}},
			FuzzyReferencesStatistics:    &eventStatistics{EventLatencies: &eventLatencies{}},
		})
	}

	eventNames := map[string]func(p *usagePeriod) *eventStatistics{
		"codeintel.hover.precise":       func(p *usagePeriod) *eventStatistics { return p.PreciseHoverStatistics },
		"codeintel.hover.fuzzy":         func(p *usagePeriod) *eventStatistics { return p.FuzzyHoverStatistics },
		"codeintel.definitions.precise": func(p *usagePeriod) *eventStatistics { return p.PreciseDefinitionsStatistics },
		"codeintel.definitions.fuzzy":   func(p *usagePeriod) *eventStatistics { return p.FuzzyDefinitionsStatistics },
		"codeintel.references.precise":  func(p *usagePeriod) *eventStatistics { return p.PreciseReferencesStatistics },
		"codeintel.references.fuzzy":    func(p *usagePeriod) *eventStatistics { return p.FuzzyReferencesStatistics },
	}

	for eventName, getEventStatistic := range eventNames {
		userCounts, err := db.EventLogs.CountUniqueUsersPerPeriod(ctx, periodType, startDate, periods, &db.CountUniqueUsersOptions{
			ByEventName: &eventName,
		})
		if err != nil {
			return nil, err
		}

		for i, uc := range userCounts {
			activityPeriods[i].StartTime = uc.Start
			getEventStatistic(activityPeriods[i]).UsersCount = int32(uc.Count)
		}

		if includeEventCounts {
			eventCounts, err := db.EventLogs.CountEventsPerPeriod(ctx, periodType, startDate, periods, &db.CountEventsOptions{
				ByEventName: &eventName,
			})
			if err != nil {
				return nil, err
			}

			for i, uc := range eventCounts {
				count := int32(uc.Count)
				getEventStatistic(activityPeriods[i]).EventsCount = &count
			}
		}

		if includeEventLatencies {
			percentiles, err := db.EventLogs.PercentilesPerPeriod(ctx, periodType, startDate, periods, DurationField, DurationPercentiles, &db.PercentilesOptions{
				ByEventName: &eventName,
			})
			if err != nil {
				return nil, err
			}

			for i, p := range percentiles {
				getEventStatistic(activityPeriods[i]).EventLatencies.P50 = p.Values[0]
				getEventStatistic(activityPeriods[i]).EventLatencies.P90 = p.Values[1]
				getEventStatistic(activityPeriods[i]).EventLatencies.P99 = p.Values[2]
			}
		}
	}

	return activityPeriods, nil
}
