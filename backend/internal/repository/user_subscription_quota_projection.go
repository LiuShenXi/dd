package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

// This is display-only metadata for a parent whose optional Group edge was
// filtered by soft deletion. It does not authorize access or restore the group.
func (r *userSubscriptionRepository) GetSubscriptionQuotaMode(ctx context.Context, groupID int64) (string, error) {
	return clientFromContext(ctx, r.client).Group.Query().Where(group.IDEQ(groupID)).
		Select(group.FieldSubscriptionQuotaMode).String(mixins.SkipSoftDelete(ctx))
}
