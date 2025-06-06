// GoToSocial
// Copyright (C) GoToSocial Authors admin@gotosocial.org
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package v1

import (
	"errors"

	apimodel "code.superseriousbusiness.org/gotosocial/internal/api/model"
	apiutil "code.superseriousbusiness.org/gotosocial/internal/api/util"
	"code.superseriousbusiness.org/gotosocial/internal/util"
	"code.superseriousbusiness.org/gotosocial/internal/validate"
)

func validateNormalizeCreateUpdateFilter(form *apimodel.FilterCreateUpdateRequestV1) error {
	if err := validate.FilterKeyword(form.Phrase); err != nil {
		return err
	}
	// For filter v1 forwards compatibility, the phrase is used as the title of a v2 filter, so it must pass that as well.
	if err := validate.FilterTitle(form.Phrase); err != nil {
		return err
	}
	if err := validate.FilterContexts(form.Context); err != nil {
		return err
	}

	// Apply defaults for missing fields.
	form.WholeWord = util.Ptr(util.PtrOrValue(form.WholeWord, false))
	form.Irreversible = util.Ptr(util.PtrOrValue(form.Irreversible, false))

	if *form.Irreversible {
		return errors.New("irreversible aka server-side drop filters are not supported yet")
	}

	// If `expires_in` was provided
	// as JSON, then normalize it.
	if form.ExpiresInI.IsSpecified() {
		var err error
		form.ExpiresIn, err = apiutil.ParseNullableDuration(
			form.ExpiresInI,
			"expires_in",
		)
		if err != nil {
			return err
		}
	}

	return nil
}
