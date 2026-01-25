// Package domain contains the core domain types for Flesh and Blood card data.
package domain

import "slices"

// Card represents a unique Flesh and Blood card.
type Card struct {
	UniqueID                     string     `json:"unique_id"`
	Name                         string     `json:"name"`
	Color                        string     `json:"color"`
	Pitch                        string     `json:"pitch"`
	Cost                         string     `json:"cost"`
	Power                        string     `json:"power"`
	Defense                      string     `json:"defense"`
	Health                       string     `json:"health"`
	Intelligence                 string     `json:"intelligence"`
	Arcane                       string     `json:"arcane"`
	Types                        []string   `json:"types"`
	Traits                       []string   `json:"traits"`
	CardKeywords                 []string   `json:"card_keywords"`
	AbilitiesAndEffects          []string   `json:"abilities_and_effects"`
	AbilityAndEffectKeywords     []string   `json:"ability_and_effect_keywords"`
	GrantedKeywords              []string   `json:"granted_keywords"`
	RemovedKeywords              []string   `json:"removed_keywords"`
	InteractsWithKeywords        []string   `json:"interacts_with_keywords"`
	FunctionalText               string     `json:"functional_text"`
	FunctionalTextPlain          string     `json:"functional_text_plain"`
	TypeText                     string     `json:"type_text"`
	PlayedHorizontally           bool       `json:"played_horizontally"`
	BlitzLegal                   bool       `json:"blitz_legal"`
	CCLegal                      bool       `json:"cc_legal"`
	CommonerLegal                bool       `json:"commoner_legal"`
	LLLegal                      bool       `json:"ll_legal"`
	SilverAgeLegal               bool       `json:"silver_age_legal"`
	BlitzLivingLegend            bool       `json:"blitz_living_legend"`
	BlitzLivingLegendStart       string     `json:"blitz_living_legend_start,omitempty"`
	CCLivingLegend               bool       `json:"cc_living_legend"`
	CCLivingLegendStart          string     `json:"cc_living_legend_start,omitempty"`
	BlitzBanned                  bool       `json:"blitz_banned"`
	BlitzBannedStart             string     `json:"blitz_banned_start,omitempty"`
	CCBanned                     bool       `json:"cc_banned"`
	CCBannedStart                string     `json:"cc_banned_start,omitempty"`
	CommonerBanned               bool       `json:"commoner_banned"`
	CommonerBannedStart          string     `json:"commoner_banned_start,omitempty"`
	LLBanned                     bool       `json:"ll_banned"`
	LLBannedStart                string     `json:"ll_banned_start,omitempty"`
	SilverAgeBanned              bool       `json:"silver_age_banned"`
	SilverAgeBannedStart         string     `json:"silver_age_banned_start,omitempty"`
	UPFBanned                    bool       `json:"upf_banned"`
	UPFBannedStart               string     `json:"upf_banned_start,omitempty"`
	BlitzSuspended               bool       `json:"blitz_suspended"`
	BlitzSuspendedStart          string     `json:"blitz_suspended_start,omitempty"`
	BlitzSuspendedEnd            string     `json:"blitz_suspended_end,omitempty"`
	CCSuspended                  bool       `json:"cc_suspended"`
	CCSuspendedStart             string     `json:"cc_suspended_start,omitempty"`
	CCSuspendedEnd               string     `json:"cc_suspended_end,omitempty"`
	CommonerSuspended            bool       `json:"commoner_suspended"`
	CommonerSuspendedStart       string     `json:"commoner_suspended_start,omitempty"`
	CommonerSuspendedEnd         string     `json:"commoner_suspended_end,omitempty"`
	LLRestricted                 bool       `json:"ll_restricted"`
	LLRestrictedAffectsFullCycle bool       `json:"ll_restricted_affects_full_cycle,omitempty"`
	LLRestrictedStart            string     `json:"ll_restricted_start,omitempty"`
	ReferencedCards              []string   `json:"referenced_cards,omitempty"`
	CardsReferencedBy            []string   `json:"cards_referenced_by,omitempty"`
	Printings                    []Printing `json:"printings"`
}

// Printing represents a specific printing of a card.
type Printing struct {
	UniqueID             string            `json:"unique_id"`
	SetPrintingUniqueID  string            `json:"set_printing_unique_id"`
	ID                   string            `json:"id"`
	SetID                string            `json:"set_id"`
	Edition              string            `json:"edition"`
	Foiling              string            `json:"foiling"`
	Rarity               string            `json:"rarity"`
	ExpansionSlot        bool              `json:"expansion_slot"`
	Artists              []string          `json:"artists"`
	ArtVariations        []string          `json:"art_variations"`
	FlavorText           string            `json:"flavor_text"`
	FlavorTextPlain      string            `json:"flavor_text_plain"`
	ImageURL             *string           `json:"image_url"`
	ImageRotationDegrees int               `json:"image_rotation_degrees"`
	TCGPlayerProductID   *string           `json:"tcgplayer_product_id"`
	TCGPlayerURL         *string           `json:"tcgplayer_url"`
	DoubleSidedCardInfo  []DoubleSidedInfo `json:"double_sided_card_info,omitempty"`
}

// DoubleSidedInfo contains information about double-sided cards.
type DoubleSidedInfo struct {
	OtherFaceUniqueID string `json:"other_face_unique_id"`
	IsFront           bool   `json:"is_front"`
	IsDFC             bool   `json:"is_DFC"`
}

// Set represents a Flesh and Blood card set.
type Set struct {
	UniqueID  string        `json:"unique_id"`
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Printings []SetPrinting `json:"printings"`
}

// SetPrinting represents a specific printing/edition of a set.
type SetPrinting struct {
	UniqueID           string  `json:"unique_id"`
	Edition            string  `json:"edition"`
	StartCardID        string  `json:"start_card_id"`
	EndCardID          string  `json:"end_card_id"`
	InitialReleaseDate string  `json:"initial_release_date"`
	OutOfPrint         bool    `json:"out_of_print"`
	CardDatabase       *string `json:"card_database"`
	ProductPage        *string `json:"product_page"`
	CollectorsCenter   *string `json:"collectors_center"`
	CardGallery        *string `json:"card_gallery"`
	ReleaseNotes       *string `json:"release_notes"`
	SetLogo            *string `json:"set_logo"`
}

// Keyword represents a game keyword with its description.
type Keyword struct {
	UniqueID         string `json:"unique_id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	DescriptionPlain string `json:"description_plain"`
}

// Ability represents a card ability type.
type Ability struct {
	UniqueID string `json:"unique_id"`
	Name     string `json:"name"`
}

// Type represents a card type.
type Type struct {
	UniqueID string `json:"unique_id"`
	Name     string `json:"name"`
}

// Rarity represents a card rarity.
type Rarity struct {
	UniqueID string `json:"unique_id"`
	ID       string `json:"id"`
	Name     string `json:"name"`
}

// Format represents a game format for legality checks.
type Format string

const (
	FormatBlitz     Format = "blitz"
	FormatCC        Format = "cc"
	FormatCommoner  Format = "commoner"
	FormatLL        Format = "ll"
	FormatSilverAge Format = "silver_age"
	FormatUPF       Format = "upf"
)

// Legality represents a card's legality status in a format.
type Legality struct {
	Format       Format `json:"format"`
	Legal        bool   `json:"legal"`
	LivingLegend bool   `json:"living_legend,omitempty"`
	Banned       bool   `json:"banned,omitempty"`
	Suspended    bool   `json:"suspended,omitempty"`
	Restricted   bool   `json:"restricted,omitempty"`
}

// GetLegality returns the legality information for a card in a given format.
func (c *Card) GetLegality(format Format) Legality {
	switch format {
	case FormatBlitz:
		return Legality{
			Format:       format,
			Legal:        c.BlitzLegal && !c.BlitzBanned && !c.BlitzSuspended && !c.BlitzLivingLegend,
			LivingLegend: c.BlitzLivingLegend,
			Banned:       c.BlitzBanned,
			Suspended:    c.BlitzSuspended,
		}
	case FormatCC:
		return Legality{
			Format:       format,
			Legal:        c.CCLegal && !c.CCBanned && !c.CCSuspended && !c.CCLivingLegend,
			LivingLegend: c.CCLivingLegend,
			Banned:       c.CCBanned,
			Suspended:    c.CCSuspended,
		}
	case FormatCommoner:
		return Legality{
			Format:    format,
			Legal:     c.CommonerLegal && !c.CommonerBanned && !c.CommonerSuspended,
			Banned:    c.CommonerBanned,
			Suspended: c.CommonerSuspended,
		}
	case FormatLL:
		return Legality{
			Format:     format,
			Legal:      c.LLLegal && !c.LLBanned && !c.LLRestricted,
			Banned:     c.LLBanned,
			Restricted: c.LLRestricted,
		}
	case FormatSilverAge:
		return Legality{
			Format: format,
			Legal:  c.SilverAgeLegal && !c.SilverAgeBanned,
			Banned: c.SilverAgeBanned,
		}
	case FormatUPF:
		return Legality{
			Format: format,
			Legal:  !c.UPFBanned,
			Banned: c.UPFBanned,
		}
	default:
		return Legality{Format: format}
	}
}

// HasType checks if a card has a specific type.
func (c *Card) HasType(typeName string) bool {
	return slices.Contains(c.Types, typeName)
}

// HasKeyword checks if a card has a specific keyword.
func (c *Card) HasKeyword(keyword string) bool {
	return slices.Contains(c.CardKeywords, keyword)
}

// GetClass returns the class of the card (first type that is a class).
func (c *Card) GetClass() string {
	classes := map[string]bool{
		"Generic": true, "Warrior": true, "Brute": true, "Guardian": true,
		"Ninja": true, "Mechanologist": true, "Ranger": true, "Runeblade": true,
		"Wizard": true, "Illusionist": true, "Elemental": true, "Light": true,
		"Shadow": true, "Ice": true, "Lightning": true, "Earth": true,
		"Mystic": true, "Assassin": true, "Shapeshifter": true, "Bard": true,
		"Adjudicator": true, "Necromancer": true, "Draconic": true, "Royal": true,
	}
	for _, t := range c.Types {
		if classes[t] {
			return t
		}
	}
	return ""
}
