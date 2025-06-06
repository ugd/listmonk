package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/knadh/listmonk/internal/manager"
	"github.com/knadh/listmonk/models"
	"github.com/labstack/echo/v4"
)

// SendTxMessage handles the sending of a transactional message.
func (a *App) SendTxMessage(c echo.Context) error {
	var m models.TxMessage

	// If it's a multipart form, there may be file attachments.
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "multipart/form-data") {
		form, err := c.MultipartForm()
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", err.Error()))
		}

		data, ok := form.Value["data"]
		if !ok || len(data) != 1 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "data"))
		}

		// Parse the JSON data.
		if err := json.Unmarshal([]byte(data[0]), &m); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("data: %s", err.Error())))
		}

		// Attach files.
		for _, f := range form.File["file"] {
			file, err := f.Open()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError,
					a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("file: %s", err.Error())))
			}
			defer file.Close()

			b, err := io.ReadAll(file)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError,
					a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("file: %s", err.Error())))
			}

			m.Attachments = append(m.Attachments, models.Attachment{
				Name:    f.Filename,
				Header:  manager.MakeAttachmentHeader(f.Filename, "base64", f.Header.Get("Content-Type")),
				Content: b,
			})
		}

	} else if err := c.Bind(&m); err != nil {
		return err
	}

	// Validate fields.
	if r, err := a.validateTxMessage(m); err != nil {
		return err
	} else {
		m = r
	}

	// Get the cached tx template.
	tpl, err := a.manager.GetTpl(m.TemplateID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.notFound", "name", fmt.Sprintf("template %d", m.TemplateID)))
	}

	var (
		num      = len(m.SubscriberEmails)
		isEmails = true
	)
	if len(m.SubscriberIDs) > 0 {
		num = len(m.SubscriberIDs)
		isEmails = false
	}

	notFound := []string{}
	for n := range num {
		var (
			subID    int
			subEmail string
		)

		if !isEmails {
			subID = m.SubscriberIDs[n]
		} else {
			subEmail = m.SubscriberEmails[n]
		}

		// Get the subscriber.
		sub, err := a.core.GetSubscriber(subID, "", subEmail)
		if err != nil {
			// If the subscriber is not found, log that error and move on without halting on the list.
			if er, ok := err.(*echo.HTTPError); ok && er.Code == http.StatusBadRequest {
				notFound = append(notFound, fmt.Sprintf("%v", er.Message))
				continue
			}

			return err
		}

		// Render the message.
		if err := m.Render(sub, tpl); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.errorFetching", "name"))
		}

		// Prepare the final message.
		msg := models.Message{}
		msg.Subscriber = sub
		msg.To = []string{sub.Email}
		msg.From = m.FromEmail
		msg.Subject = m.Subject
		msg.ContentType = m.ContentType
		msg.Messenger = m.Messenger
		msg.Body = m.Body
		for _, a := range m.Attachments {
			msg.Attachments = append(msg.Attachments, models.Attachment{
				Name:    a.Name,
				Header:  a.Header,
				Content: a.Content,
			})
		}

		// Optional headers.
		if len(m.Headers) != 0 {
			msg.Headers = make(textproto.MIMEHeader, len(m.Headers))
			for _, set := range m.Headers {
				for hdr, val := range set {
					msg.Headers.Add(hdr, val)
				}
			}
		}

		if err := a.manager.PushMessage(msg); err != nil {
			a.log.Printf("error sending message (%s): %v", msg.Subject, err)
			return err
		}
	}

	if len(notFound) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, strings.Join(notFound, "; "))
	}

	return c.JSON(http.StatusOK, okResp{true})
}

// validateTxMessage validates the tx message fields.
func (a *App) validateTxMessage(m models.TxMessage) (models.TxMessage, error) {
	if len(m.SubscriberEmails) > 0 && m.SubscriberEmail != "" {
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "do not send both `subscriber_email` and `subscriber_emails`"))
	}
	if len(m.SubscriberIDs) > 0 && m.SubscriberID != 0 {
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "do not send both `subscriber_id` and `subscriber_ids`"))
	}

	if m.SubscriberEmail != "" {
		m.SubscriberEmails = append(m.SubscriberEmails, m.SubscriberEmail)
	}

	if m.SubscriberID != 0 {
		m.SubscriberIDs = append(m.SubscriberIDs, m.SubscriberID)
	}

	if (len(m.SubscriberEmails) == 0 && len(m.SubscriberIDs) == 0) || (len(m.SubscriberEmails) > 0 && len(m.SubscriberIDs) > 0) {
		return m, echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "send subscriber_emails OR subscriber_ids"))
	}

	for n, email := range m.SubscriberEmails {
		em, err := a.importer.SanitizeEmail(email)
		if err != nil {
			return m, echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		m.SubscriberEmails[n] = em
	}

	if m.FromEmail == "" {
		m.FromEmail = a.cfg.FromEmail
	}

	if m.Messenger == "" {
		m.Messenger = emailMsgr
	} else if !a.manager.HasMessenger(m.Messenger) {
		return m, echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("campaigns.fieldInvalidMessenger", "name", m.Messenger))
	}

	return m, nil
}

// SendExternalTxMessage handles the sending of a transactional message to external email addresses.
func (a *App) SendExternalTxMessage(c echo.Context) error {
	var m models.TxMessage

	// If it's a multipart form, there may be file attachments.
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "multipart/form-data") {
		form, err := c.MultipartForm()
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", err.Error()))
		}

		data, ok := form.Value["data"]
		if !ok || len(data) != 1 {
			return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("globals.messages.invalidFields", "name", "data"))
		}

		// Parse the JSON data.
		if err := json.Unmarshal([]byte(data[0]), &m); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest,
				a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("data: %s", err.Error())))
		}

		// Attach files.
		for _, f := range form.File["file"] {
			file, err := f.Open()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError,
					a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("file: %s", err.Error())))
			}
			defer file.Close()

			b, err := io.ReadAll(file)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError,
					a.i18n.Ts("globals.messages.invalidFields", "name", fmt.Sprintf("file: %s", err.Error())))
			}

			m.Attachments = append(m.Attachments, models.Attachment{
				Name:    f.Filename,
				Header:  manager.MakeAttachmentHeader(f.Filename, "base64", f.Header.Get("Content-Type")),
				Content: b,
			})
		}

	} else if err := c.Bind(&m); err != nil {
		return err
	}

	// Check for recipient fields first
	if len(m.RecipientEmails) == 0 && m.RecipientEmail == "" {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.invalidFields", "name", "recipient_emails or recipient_email is required"))
	}

	// Convert recipient_email to recipient_emails if needed
	if m.RecipientEmail != "" {
		m.RecipientEmails = append(m.RecipientEmails, m.RecipientEmail)
	}

	// Sanitize all recipient emails
	for n, email := range m.RecipientEmails {
		em, err := a.importer.SanitizeEmail(email)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		m.RecipientEmails[n] = em
	}

	// Copy recipient emails to subscriber emails for internal processing
	m.SubscriberEmails = m.RecipientEmails

	// Set default values
	if m.FromEmail == "" {
		m.FromEmail = a.cfg.FromEmail
	}

	if m.Messenger == "" {
		m.Messenger = emailMsgr
	} else if !a.manager.HasMessenger(m.Messenger) {
		return echo.NewHTTPError(http.StatusBadRequest, a.i18n.Ts("campaigns.fieldInvalidMessenger", "name", m.Messenger))
	}

	// Get the cached tx template.
	tpl, err := a.manager.GetTpl(m.TemplateID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.notFound", "name", fmt.Sprintf("template %d", m.TemplateID)))
	}

	// Create a dummy subscriber for template rendering.
	dummySub := models.Subscriber{
		Email: m.SubscriberEmails[0],
		Name:  m.SubscriberEmails[0],
	}

	// Render the message.
	if err := m.Render(dummySub, tpl); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			a.i18n.Ts("globals.messages.errorFetching", "name"))
	}

	// Prepare the final message.
	msg := models.Message{}
	msg.To = m.SubscriberEmails
	msg.From = m.FromEmail
	msg.Subject = m.Subject
	msg.ContentType = m.ContentType
	msg.Messenger = m.Messenger
	msg.Body = m.Body
	for _, a := range m.Attachments {
		msg.Attachments = append(msg.Attachments, models.Attachment{
			Name:    a.Name,
			Header:  a.Header,
			Content: a.Content,
		})
	}

	// Optional headers.
	if len(m.Headers) != 0 {
		msg.Headers = make(textproto.MIMEHeader, len(m.Headers))
		for _, set := range m.Headers {
			for hdr, val := range set {
				msg.Headers.Add(hdr, val)
			}
		}
	}

	if err := a.manager.PushMessage(msg); err != nil {
		a.log.Printf("error sending message (%s): %v", msg.Subject, err)
		return err
	}

	return c.JSON(http.StatusOK, okResp{true})
}
