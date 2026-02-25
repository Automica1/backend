// internal/services/email_service.go
package services

import (
	"fmt"
	"time"

	gomail "gopkg.in/mail.v2"
)

type EmailService interface {
	SendSubscriptionConfirmation(toEmail, toName, planName string, credits int, nextBillingDate time.Time) error
	SendPaymentFailed(toEmail, toName string, retryDate time.Time) error
	SendCancellationConfirmation(toEmail, toName string, accessUntil time.Time) error
	SendSubscriptionExpired(toEmail, toName string) error
}

type emailService struct {
	fromEmail string
	fromName  string
	password  string
	host      string
	port      int
}

func NewEmailService(fromEmail, fromName, password string) EmailService {
	return &emailService{
		fromEmail: fromEmail,
		fromName:  fromName,
		password:  password,
		host:      "smtp.gmail.com",
		port:      587,
	}
}

func (s *emailService) send(toEmail, subject, htmlBody string) error {
	m := gomail.NewMessage()
	m.SetAddressHeader("From", s.fromEmail, s.fromName)
	m.SetHeader("To", toEmail)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", htmlBody)

	d := gomail.NewDialer(s.host, s.port, s.fromEmail, s.password)

	if err := d.DialAndSend(m); err != nil {
		fmt.Printf("[EmailService] Failed to send email to %s: %v\n", toEmail, err)
		return err
	}

	fmt.Printf("[EmailService] Email sent successfully to %s: %s\n", toEmail, subject)
	return nil
}

// --- Templates ---

func (s *emailService) SendSubscriptionConfirmation(toEmail, toName, planName string, credits int, nextBillingDate time.Time) error {
	subject := "✅ Subscription Activated — Automica"
	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body style="font-family: Arial, sans-serif; background:#f9f9f9; margin:0; padding:0;">
  <div style="max-width:600px; margin:40px auto; background:#fff; border-radius:12px; overflow:hidden; box-shadow:0 2px 8px rgba(0,0,0,0.08);">
    <div style="background: linear-gradient(135deg, #7c3aed, #4f46e5); padding:32px; text-align:center;">
      <h1 style="color:#fff; margin:0; font-size:24px;">You're now subscribed! 🎉</h1>
    </div>
    <div style="padding:32px;">
      <p style="color:#333; font-size:16px;">Hi %s,</p>
      <p style="color:#555;">Your <strong>%s</strong> subscription is now active. Here's a summary:</p>
      <div style="background:#f3f0ff; border-radius:8px; padding:20px; margin:20px 0;">
        <p style="margin:0; color:#333;"><strong>Credits Added:</strong> %s</p>
        <p style="margin:8px 0 0; color:#333;"><strong>Next Billing Date:</strong> %s</p>
      </div>
      <p style="color:#555;">You can use your credits to access all Automica services. Log in to check your balance.</p>
      <div style="text-align:center; margin-top:28px;">
        <a href="https://automica.ai/dashboard" style="background:#7c3aed; color:#fff; padding:12px 28px; border-radius:8px; text-decoration:none; font-weight:bold;">Go to Dashboard</a>
      </div>
    </div>
    <div style="background:#f3f0ff; padding:16px; text-align:center;">
      <p style="color:#888; font-size:12px; margin:0;">Automica AI · automica.ai · You're receiving this because you subscribed.</p>
    </div>
  </div>
</body>
</html>
`, toName, planName, formatCredits(credits), nextBillingDate.Format("January 2, 2006"))

	return s.send(toEmail, subject, body)
}

func (s *emailService) SendPaymentFailed(toEmail, toName string, retryDate time.Time) error {
	subject := "⚠️ Payment Failed — Action Required"
	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body style="font-family: Arial, sans-serif; background:#f9f9f9; margin:0; padding:0;">
  <div style="max-width:600px; margin:40px auto; background:#fff; border-radius:12px; overflow:hidden; box-shadow:0 2px 8px rgba(0,0,0,0.08);">
    <div style="background: linear-gradient(135deg, #dc2626, #b91c1c); padding:32px; text-align:center;">
      <h1 style="color:#fff; margin:0; font-size:24px;">Payment Failed ⚠️</h1>
    </div>
    <div style="padding:32px;">
      <p style="color:#333; font-size:16px;">Hi %s,</p>
      <p style="color:#555;">We were unable to process your subscription payment. Don't worry — your subscription is still active and we'll automatically retry.</p>
      <div style="background:#fff5f5; border-radius:8px; border-left:4px solid #dc2626; padding:16px; margin:20px 0;">
        <p style="margin:0; color:#333;"><strong>Next Retry:</strong> %s</p>
        <p style="margin:8px 0 0; color:#555; font-size:14px;">Please ensure your payment method is up to date before this date.</p>
      </div>
      <p style="color:#555;">Your credits remain fully usable during this period.</p>
      <div style="text-align:center; margin-top:28px;">
        <a href="https://automica.ai/subscription" style="background:#dc2626; color:#fff; padding:12px 28px; border-radius:8px; text-decoration:none; font-weight:bold;">Update Payment Method</a>
      </div>
    </div>
    <div style="background:#f3f0ff; padding:16px; text-align:center;">
      <p style="color:#888; font-size:12px; margin:0;">Automica AI · automica.ai</p>
    </div>
  </div>
</body>
</html>
`, toName, retryDate.Format("January 2, 2006"))

	return s.send(toEmail, subject, body)
}

func (s *emailService) SendCancellationConfirmation(toEmail, toName string, accessUntil time.Time) error {
	subject := "Subscription Cancelled — Automica"
	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body style="font-family: Arial, sans-serif; background:#f9f9f9; margin:0; padding:0;">
  <div style="max-width:600px; margin:40px auto; background:#fff; border-radius:12px; overflow:hidden; box-shadow:0 2px 8px rgba(0,0,0,0.08);">
    <div style="background: linear-gradient(135deg, #6b7280, #374151); padding:32px; text-align:center;">
      <h1 style="color:#fff; margin:0; font-size:24px;">Subscription Cancelled</h1>
    </div>
    <div style="padding:32px;">
      <p style="color:#333; font-size:16px;">Hi %s,</p>
      <p style="color:#555;">Your Automica subscription has been cancelled. Here's what happens next:</p>
      <div style="background:#f9fafb; border-radius:8px; padding:20px; margin:20px 0;">
        <p style="margin:0; color:#333;">✅ Your subscription remains <strong>active until %s</strong></p>
        <p style="margin:8px 0 0; color:#333;">✅ Your credits remain fully usable until then</p>
        <p style="margin:8px 0 0; color:#333;">❌ You will not be charged again</p>
      </div>
      <p style="color:#555;">We're sorry to see you go. You can resubscribe at any time.</p>
      <div style="text-align:center; margin-top:28px;">
        <a href="https://automica.ai/subscription" style="background:#7c3aed; color:#fff; padding:12px 28px; border-radius:8px; text-decoration:none; font-weight:bold;">Resubscribe</a>
      </div>
    </div>
    <div style="background:#f3f0ff; padding:16px; text-align:center;">
      <p style="color:#888; font-size:12px; margin:0;">Automica AI · automica.ai</p>
    </div>
  </div>
</body>
</html>
`, toName, accessUntil.Format("January 2, 2006"))

	return s.send(toEmail, subject, body)
}

func (s *emailService) SendSubscriptionExpired(toEmail, toName string) error {
	subject := "Your Automica Subscription Has Expired"
	body := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body style="font-family: Arial, sans-serif; background:#f9f9f9; margin:0; padding:0;">
  <div style="max-width:600px; margin:40px auto; background:#fff; border-radius:12px; overflow:hidden; box-shadow:0 2px 8px rgba(0,0,0,0.08);">
    <div style="background: linear-gradient(135deg, #9333ea, #7c3aed); padding:32px; text-align:center;">
      <h1 style="color:#fff; margin:0; font-size:24px;">Subscription Expired</h1>
    </div>
    <div style="padding:32px;">
      <p style="color:#333; font-size:16px;">Hi %s,</p>
      <p style="color:#555;">Your Automica subscription has expired after all payment retries were unsuccessful. Your credit balance has been reset.</p>
      <div style="background:#f9fafb; border-radius:8px; padding:20px; margin:20px 0;">
        <p style="margin:0; color:#555;">Resubscribe to get a fresh set of credits and continue using Automica services.</p>
      </div>
      <div style="text-align:center; margin-top:28px;">
        <a href="https://automica.ai/subscription" style="background:#7c3aed; color:#fff; padding:12px 28px; border-radius:8px; text-decoration:none; font-weight:bold;">Resubscribe Now</a>
      </div>
    </div>
    <div style="background:#f3f0ff; padding:16px; text-align:center;">
      <p style="color:#888; font-size:12px; margin:0;">Automica AI · automica.ai</p>
    </div>
  </div>
</body>
</html>
`, toName)

	return s.send(toEmail, subject, body)
}

func formatCredits(n int) string {
	return fmt.Sprintf("%d credits", n)
}
