/*
   GoToSocial
   Copyright (C) 2021-2023 GoToSocial Authors admin@gotosocial.org

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package status

import (
	"context"
	"errors"
	"fmt"

	"github.com/superseriousbusiness/gotosocial/internal/ap"
	apimodel "github.com/superseriousbusiness/gotosocial/internal/api/model"
	"github.com/superseriousbusiness/gotosocial/internal/db"
	"github.com/superseriousbusiness/gotosocial/internal/gtserror"
	"github.com/superseriousbusiness/gotosocial/internal/gtsmodel"
	"github.com/superseriousbusiness/gotosocial/internal/id"
	"github.com/superseriousbusiness/gotosocial/internal/messages"
	"github.com/superseriousbusiness/gotosocial/internal/uris"
)

func (p *processor) Fave(ctx context.Context, requestingAccount *gtsmodel.Account, targetStatusID string) (*apimodel.Status, gtserror.WithCode) {
	targetStatus, err := p.db.GetStatusByID(ctx, targetStatusID)
	if err != nil {
		return nil, gtserror.NewErrorNotFound(fmt.Errorf("error fetching status %s: %s", targetStatusID, err))
	}
	if targetStatus.Account == nil {
		return nil, gtserror.NewErrorNotFound(fmt.Errorf("no status owner for status %s", targetStatusID))
	}

	visible, err := p.filter.StatusVisible(ctx, targetStatus, requestingAccount)
	if err != nil {
		return nil, gtserror.NewErrorNotFound(fmt.Errorf("error seeing if status %s is visible: %s", targetStatus.ID, err))
	}
	if !visible {
		return nil, gtserror.NewErrorNotFound(errors.New("status is not visible"))
	}
	if !*targetStatus.Likeable {
		return nil, gtserror.NewErrorForbidden(errors.New("status is not faveable"))
	}

	// first check if the status is already faved, if so we don't need to do anything
	newFave := true
	gtsFave := &gtsmodel.StatusFave{}
	if err := p.db.GetWhere(ctx, []db.Where{{Key: "status_id", Value: targetStatus.ID}, {Key: "account_id", Value: requestingAccount.ID}}, gtsFave); err == nil {
		// we already have a fave for this status
		newFave = false
	}

	if newFave {
		thisFaveID := id.NewULID()

		// we need to create a new fave in the database
		gtsFave := &gtsmodel.StatusFave{
			ID:              thisFaveID,
			AccountID:       requestingAccount.ID,
			Account:         requestingAccount,
			TargetAccountID: targetStatus.AccountID,
			TargetAccount:   targetStatus.Account,
			StatusID:        targetStatus.ID,
			Status:          targetStatus,
			URI:             uris.GenerateURIForLike(requestingAccount.Username, thisFaveID),
		}

		if err := p.db.Put(ctx, gtsFave); err != nil {
			return nil, gtserror.NewErrorInternalError(fmt.Errorf("error putting fave in database: %s", err))
		}

		// send it back to the processor for async processing
		p.clientWorker.Queue(messages.FromClientAPI{
			APObjectType:   ap.ActivityLike,
			APActivityType: ap.ActivityCreate,
			GTSModel:       gtsFave,
			OriginAccount:  requestingAccount,
			TargetAccount:  targetStatus.Account,
		})
	}

	// return the apidon representation of the target status
	apiStatus, err := p.tc.StatusToAPIStatus(ctx, targetStatus, requestingAccount)
	if err != nil {
		return nil, gtserror.NewErrorInternalError(fmt.Errorf("error converting status %s to frontend representation: %s", targetStatus.ID, err))
	}

	return apiStatus, nil
}
