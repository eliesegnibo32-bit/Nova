// Meta webhook payload types (spec: internal/whatsapp/types.go).
//
// This file exposes the Meta Cloud API webhook envelope under the spec-named
// types (WebhookPayload, Entry, Change, Value, Metadata, Message, TextContent,
// MediaContent, Status, Conversation, Origin, Pricing). These are TYPE
// ALIASES of the WebhookEvent / WebhookEntry / ... types declared in
// webhook.go — they refer to the same underlying types so the rest of the
// codebase can use either name interchangeably. We also expose the
// `ExtractInboundMessages()` and `ExtractStatusUpdates()` helpers that the
// spec requires.
//
// The full envelope is documented in webhook.go.
package whatsapp

import "strings"

// WebhookPayload is the spec name for WebhookEvent (top-level envelope).
type WebhookPayload = WebhookEvent

// Entry is the spec name for WebhookEntry (one entry per WABA).
type Entry = WebhookEntry

// Change is the spec name for WebhookChange.
type Change = WebhookChange

// Value is the spec name for WebhookValue.
type Value = WebhookValue

// Metadata is the spec name for WebhookMetadata.
type Metadata = WebhookMetadata

// Message is the spec name for WhatsAppMessage.
type Message = WhatsAppMessage

// TextContent is the spec name for the inline text body struct on Message.
// Since Message.Text is an anonymous struct, we expose a named alias for
// callers that want to construct one explicitly.
type TextContent = struct {
	Body string `json:"body"`
}

// MediaContent is the spec name for the inline media struct on Message.
// Re-exported as an alias of the anonymous struct used by Message.Image.
type MediaContent = struct {
	ID      string `json:"id"`
	Caption string `json:"caption,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

// ButtonContent is the spec name for the inline button struct on Message.
type ButtonContent = struct {
	Text    string `json:"text"`
	Payload string `json:"payload"`
}

// InteractiveContent is the spec name for the inline interactive struct on
// Message.
type InteractiveContent = struct {
	Type        string `json:"type"` // button_reply | list_reply
	ButtonReply *struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"button_reply,omitempty"`
	ListReply *struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
	} `json:"list_reply,omitempty"`
}

// Context is the spec name for the inline context struct on Message.
type Context = struct {
	From string `json:"from"`
	ID   string `json:"id"`
}

// Status is the spec name for WhatsAppStatus.
type Status = WhatsAppStatus

// Conversation is the spec name for the inline conversation struct on Status.
// (Meta embeds a "conversation" object in status updates that includes the
// origin type and the 24h window expiration timestamp.)
type Conversation = struct {
	ID                 string `json:"id"`
	Origin             Origin `json:"origin"`
	ExpirationTimestamp int64 `json:"expiration_timestamp"`
}

// Origin is the conversation origin ("user" = inbound-initiated, opens the
// 24h window; "business" = business-initiated).
type Origin = struct {
	Type string `json:"type"`
}

// Pricing is the pricing/billing metadata Meta attaches to status updates.
// Used for cost tracking (ch. 6 — Coûts).
type Pricing = struct {
	Billable       bool   `json:"billable"`
	PricingModel   string `json:"pricing_model"`
	Category       string `json:"category"`
	PricingCategory string `json:"pricing_category"`
}

// StatusError is the spec name for WhatsAppError.
type StatusError = WhatsAppError

// InboundMessage is the normalized, flat shape of one inbound customer
// message extracted from a Meta webhook envelope. The webhook processor
// consumes this list rather than walking the nested envelope itself.
type InboundMessage struct {
	ShopPhoneNumber   string // recipient's display_phone_number (for shop routing)
	PhoneNumberID     string // recipient's phone_number_id (Meta identifier)
	From              string // sender phone (E.164, digits only)
	FromName          string // sender profile name (if Meta sent it)
	Wamid             string // WhatsApp Message ID (dedup key)
	Type              string // text | image | audio | video | document | interactive | button
	Text              string // body when type=text
	MediaID           string // media ID when type=image/audio/video/document
	MediaCaption      string // media caption if present
	ButtonPayload     string // button payload when type=button
	InteractiveReply  string // button_reply.title or list_reply.title when type=interactive
	Timestamp         string // unix seconds (string)
	ContextMessageID  string // wamid of OUR message the customer replied to (if any)
}

// StatusUpdate is the normalized, flat shape of one outbound message status
// update extracted from a Meta webhook envelope.
type StatusUpdate struct {
	ShopPhoneNumber string // recipient's display_phone_number (for shop routing)
	PhoneNumberID   string // recipient's phone_number_id (Meta identifier)
	Wamid           string // WhatsApp Message ID of OUR message
	Status          string // sent | delivered | read | failed | deleted
	RecipientID     string // customer phone (digits)
	Timestamp       string // unix seconds (string)
	Conversation    *Conversation
	Pricing         *Pricing
	Errors          []StatusError
}

// ExtractInboundMessages walks the webhook envelope and returns a flat list
// of inbound customer messages, one per Message in every Entry/Change. The
// shop phone number is propagated from each Change's Metadata so the caller
// can route the message to the right shop.
//
// Returns an empty slice (not nil) when the envelope has no inbound messages
// (e.g. a status-only webhook delivery).
func (p *WebhookPayload) ExtractInboundMessages() []InboundMessage {
	out := []InboundMessage{}
	if p == nil {
		return out
	}
	for _, entry := range p.Entry {
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			shopPhone := change.Value.Metadata.DisplayPhoneNumber
			phoneID := change.Value.Metadata.PhoneNumberID
			// Build a quick lookup of wa_id → profile name from contacts.
			names := map[string]string{}
			for _, c := range change.Value.Contacts {
				names[c.WaID] = c.Profile.Name
			}
			for _, m := range change.Value.Messages {
				im := InboundMessage{
					ShopPhoneNumber: shopPhone,
					PhoneNumberID:   phoneID,
					From:            m.From,
					FromName:        names[m.From],
					Wamid:           m.ID,
					Type:            m.Type,
					Timestamp:       m.Timestamp,
				}
				if m.Text != nil {
					im.Text = m.Text.Body
				}
				if m.Image != nil {
					im.MediaID = m.Image.ID
					im.MediaCaption = m.Image.Caption
				}
				if m.Audio != nil {
					im.MediaID = m.Audio.ID
				}
				if m.Video != nil {
					im.MediaID = m.Video.ID
					im.MediaCaption = m.Video.Caption
				}
				if m.Document != nil {
					im.MediaID = m.Document.ID
					im.MediaCaption = m.Document.Caption
				}
				if m.Button != nil {
					im.ButtonPayload = m.Button.Payload
					if im.Text == "" {
						im.Text = m.Button.Text
					}
				}
				if m.Interactive != nil {
					if m.Interactive.ButtonReply != nil {
						im.InteractiveReply = m.Interactive.ButtonReply.Title
						if im.Text == "" {
							im.Text = m.Interactive.ButtonReply.Title
						}
					} else if m.Interactive.ListReply != nil {
						im.InteractiveReply = m.Interactive.ListReply.Title
						if im.Text == "" {
							im.Text = m.Interactive.ListReply.Title
						}
					}
				}
				if m.Context != nil {
					im.ContextMessageID = m.Context.ID
				}
				out = append(out, im)
			}
		}
	}
	return out
}

// ExtractStatusUpdates walks the webhook envelope and returns a flat list of
// outbound message status updates, one per Status in every Entry/Change. The
// shop phone number is propagated from each Change's Metadata so the caller
// can route the status to the right shop.
//
// Returns an empty slice (not nil) when the envelope has no status updates.
func (p *WebhookPayload) ExtractStatusUpdates() []StatusUpdate {
	out := []StatusUpdate{}
	if p == nil {
		return out
	}
	for _, entry := range p.Entry {
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			shopPhone := change.Value.Metadata.DisplayPhoneNumber
			phoneID := change.Value.Metadata.PhoneNumberID
			for _, s := range change.Value.Statuses {
				out = append(out, StatusUpdate{
					ShopPhoneNumber: shopPhone,
					PhoneNumberID:   phoneID,
					Wamid:           s.ID,
					Status:          s.Status,
					RecipientID:     s.Recipient,
					Timestamp:       s.Timestamp,
					Errors:          s.Errors,
				})
			}
		}
	}
	return out
}

// NormalizePhoneForRouting strips a leading "+" and any whitespace/dashes
// from a phone number so two strings representing the same E.164 number compare
// equal. Used by the shop-routing layer (ShopRepository.GetByWhatsAppNumber).
func NormalizePhoneForRouting(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
