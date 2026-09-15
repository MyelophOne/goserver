package goserver

import "time"

func (a *App) prunePagePlansLocked(now time.Time) {
	for len(a.pagePlanOrder) > 0 {
		token := a.pagePlanOrder[0]
		expires, exists := a.pagePlanExpiry[token]
		if exists && now.Before(expires) && len(a.pagePlans) < runtimeStateLimit {
			break
		}
		delete(a.pagePlans, token)
		delete(a.pagePlanExpiry, token)
		a.pagePlanOrder[0] = ""
		a.pagePlanOrder = a.pagePlanOrder[1:]
	}
}

func (a *App) rememberPagePlan(token string, plan ModulePlan) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pagePlans == nil {
		a.pagePlans = make(map[string]ModulePlan)
	}
	if a.pagePlanExpiry == nil {
		a.pagePlanExpiry = make(map[string]time.Time)
	}
	now := time.Now()
	a.prunePagePlansLocked(now)
	if _, exists := a.pagePlans[token]; !exists {
		a.pagePlanOrder = append(a.pagePlanOrder, token)
	}
	a.pagePlans[token] = plan
	a.pagePlanExpiry[token] = now.Add(runtimeStateTTL)
}

func (a *App) lookupPagePlan(token string) (ModulePlan, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	plan, exists := a.pagePlans[token]
	if expires, tracked := a.pagePlanExpiry[token]; tracked && !time.Now().Before(expires) {
		delete(a.pagePlans, token)
		delete(a.pagePlanExpiry, token)
		return ModulePlan{}, false
	}
	return plan, exists
}
