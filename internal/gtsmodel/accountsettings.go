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

package gtsmodel

import (
	"strings"
	"time"
)

// AccountSettings models settings / preferences for a local, non-instance account.
type AccountSettings struct {
	AccountID                      string             `bun:"type:CHAR(26),pk,nullzero,notnull,unique"`                    // AccountID that owns this settings.
	CreatedAt                      time.Time          `bun:"type:timestamptz,nullzero,notnull,default:current_timestamp"` // when was item created.
	UpdatedAt                      time.Time          `bun:"type:timestamptz,nullzero,notnull,default:current_timestamp"` // when was item was last updated.
	Privacy                        Visibility         `bun:",nullzero,default:3"`                                         // Default post privacy for this account
	Sensitive                      *bool              `bun:",nullzero,notnull,default:false"`                             // Set posts from this account to sensitive by default?
	Language                       string             `bun:",nullzero,notnull,default:'en'"`                              // What language does this account post in?
	StatusContentType              string             `bun:",nullzero"`                                                   // What is the default format for statuses posted by this account (only for local accounts).
	Theme                          string             `bun:",nullzero"`                                                   // Preset CSS theme filename selected by this Account (empty string if nothing set).
	CustomCSS                      string             `bun:",nullzero"`                                                   // Custom CSS that should be displayed for this Account's profile and statuses.
	EnableRSS                      *bool              `bun:",nullzero,notnull,default:false"`                             // enable RSS feed subscription for this account's public posts at [URL]/feed
	HideCollections                *bool              `bun:",nullzero,notnull,default:false"`                             // Hide this account's followers/following collections.
	WebLayout                      WebLayout          `bun:",nullzero,notnull,default:1"`                                 // Layout to use when showing this profile via the web.
	InteractionPolicyDirect        *InteractionPolicy `bun:""`                                                            // Interaction policy to use for new direct visibility statuses by this account. If null, assume default policy.
	InteractionPolicyMutualsOnly   *InteractionPolicy `bun:""`                                                            // Interaction policy to use for new mutuals only visibility statuses. If null, assume default policy.
	InteractionPolicyFollowersOnly *InteractionPolicy `bun:""`                                                            // Interaction policy to use for new followers only visibility statuses. If null, assume default policy.
	InteractionPolicyUnlocked      *InteractionPolicy `bun:""`                                                            // Interaction policy to use for new unlocked visibility statuses. If null, assume default policy.
	InteractionPolicyPublic        *InteractionPolicy `bun:""`                                                            // Interaction policy to use for new public visibility statuses. If null, assume default policy.
}

// WebLayout represents an account owner's
// choice for how they want their profile to be
// laid out via the web view, by default.
type WebLayout enumType

const (
	WebLayoutUnknown WebLayout = 0

	// "Classic" / default GtS microblog view.
	WebLayoutMicroblog WebLayout = 1

	// 'gram-style gallery view with media only.
	WebLayoutGallery WebLayout = 2
)

// String returns a stringified, frontend
// API compatible form of WebLayout.
func (wrm WebLayout) String() string {
	switch wrm {
	case WebLayoutMicroblog:
		return "microblog"
	case WebLayoutGallery:
		return "gallery"
	default:
		panic("invalid web layout")
	}
}

// ParseWebLayout returns a web
// layout from the given value.
func ParseWebLayout(in string) WebLayout {
	switch strings.ToLower(in) {
	case "microblog":
		return WebLayoutMicroblog
	case "gallery":
		return WebLayoutGallery
	default:
		return WebLayoutUnknown
	}
}
