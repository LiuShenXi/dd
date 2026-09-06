package service

import "time"

const CarpoolResetTimezone = "Asia/Shanghai"

var carpoolShanghaiLocation = func() *time.Location {
	location, err := time.LoadLocation(CarpoolResetTimezone)
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}()

// NextCarpoolResetSchedule applies the qualification-time rule. A qualification
// at or after 22:00 starts from the following Shanghai date.
func NextCarpoolResetSchedule(now time.Time, lastSuccessful *time.Time) (slotAt, scheduledAt time.Time) {
	localNow := now.In(carpoolShanghaiLocation)
	slotAt = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 22, 0, 0, 0, carpoolShanghaiLocation)
	for !slotAt.After(now) {
		slotAt = slotAt.AddDate(0, 0, 1)
	}

	for {
		scheduledAt = slotAt
		if lastSuccessful != nil {
			cooldownAt := lastSuccessful.Add(48 * time.Hour)
			if cooldownAt.After(scheduledAt) {
				scheduledAt = cooldownAt
			}
		}
		if scheduledAt.Before(slotAt.Add(time.Minute)) {
			return slotAt.UTC(), scheduledAt.UTC()
		}
		slotAt = slotAt.AddDate(0, 0, 1)
	}
}

func CarpoolResetExecutionState(now, slotAt, scheduledAt time.Time, lastSuccessful *time.Time) string {
	if !validCarpoolResetScheduleShape(slotAt, scheduledAt) {
		return "invalid"
	}
	if now.Before(scheduledAt) {
		return "not_due"
	}
	if now.Before(slotAt) || !now.Before(slotAt.Add(time.Minute)) {
		return "missed"
	}
	if lastSuccessful != nil && now.Before(lastSuccessful.Add(48*time.Hour)) {
		return "cooldown"
	}
	return "due"
}

func ValidCarpoolResetSchedule(slotAt, scheduledAt time.Time, lastSuccessful *time.Time) bool {
	if !validCarpoolResetScheduleShape(slotAt, scheduledAt) {
		return false
	}
	return lastSuccessful == nil || !scheduledAt.Before(lastSuccessful.Add(48*time.Hour))
}

func validCarpoolResetScheduleShape(slotAt, scheduledAt time.Time) bool {
	localSlot := slotAt.In(carpoolShanghaiLocation)
	expectedSlot := time.Date(localSlot.Year(), localSlot.Month(), localSlot.Day(), 22, 0, 0, 0, carpoolShanghaiLocation)
	if !slotAt.Equal(expectedSlot) || scheduledAt.Before(slotAt) || !scheduledAt.Before(slotAt.Add(time.Minute)) {
		return false
	}
	return true
}
