package handler

import (
	"errors"
	"slices"
	"strings"

	"github.com/bionicosmos/aegle/handler/transfer"
	"github.com/bionicosmos/aegle/model"
	"github.com/bionicosmos/aegle/service"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	sessionEmail  = "email"
	sessionStatus = "status"
)

var store *session.Store

func GetAccount(c *fiber.Ctx) error {
	email, status := getSession(c)
	account, err := model.FindAccount(email)
	if err != nil {
		return err
	}
	return c.JSON(transfer.ToAccount(&account, status))
}

func SignUp(c *fiber.Ctx) error {
	body := transfer.SignUpBody{}
	if err := c.BodyParser(&body); err != nil {
		return &ParseError{err}
	}
	account, err := service.SignUp(&body)
	if err != nil {
		if errors.Is(err, service.ErrAccountExists) {
			return fiber.NewError(fiber.StatusConflict, "The email exists.")
		}
		return err
	}
	if err := setSession(
		c,
		account.Email,
		transfer.AccountUnverified,
	); err != nil {
		return err
	}
	return toJSON(c, fiber.StatusCreated)
}

func Verify(c *fiber.Ctx) error {
	id := c.Params("id")
	email, _ := getSession(c)
	if err := service.Verify(id, email); err != nil {
		if errors.Is(err, service.ErrVerified) {
			return fiber.ErrConflict
		}
		if errors.Is(err, service.ErrLinkExpired) {
			return fiber.ErrNotFound
		}
		return err
	}
	if err := setSession(c, email, transfer.AccountSignedIn); err != nil {
		return err
	}
	return toJSON(c, fiber.StatusOK)
}

func SendVerificationLink(c *fiber.Ctx) error {
	email, _ := getSession(c)
	if err := service.SendVerificationLink(email); err != nil {
		if errors.Is(err, service.ErrVerified) {
			return fiber.ErrConflict
		}
		return err
	}
	return toJSON(c, fiber.StatusOK)
}

func SignIn(c *fiber.Ctx) error {
	body := transfer.SignInBody{}
	if err := c.BodyParser(&body); err != nil {
		return &ParseError{err}
	}
	account, err := service.SignIn(&body)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fiber.NewError(fiber.StatusNotFound, "user does not exist")
		}
		if errors.Is(err, service.ErrPassword) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return err
	}
	status := transfer.AccountSignedIn
	if account.TOTP != "" {
		status = transfer.AccountNeedMFA
	}
	if err := setSession(c, account.Email, status); err != nil {
		return err
	}
	return toJSON(c, fiber.StatusOK)
}

func CreateTOTP(c *fiber.Ctx) error {
	email, _ := getSession(c)
	body, err := service.CreateTOTP(email)
	if err != nil {
		return err
	}
	return c.JSON(body)
}

func ConfirmTOTP(c *fiber.Ctx) error {
	body := transfer.ConfirmTOTPBody{}
	if err := c.BodyParser(&body); err != nil {
		return &ParseError{err}
	}
	email, _ := getSession(c)
	if err := service.ConfirmTOTP(email, body.Code); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fiber.NewError(
				fiber.StatusBadRequest,
				"Uninitialized or expired TOTP",
			)
		}
		if errors.Is(err, service.ErrInvalidTOTP) {
			return fiber.NewError(fiber.StatusUnauthorized, "Invalid TOTP")
		}
		return err
	}
	return toJSON(c, fiber.StatusCreated)
}

func DeleteTOTP(c *fiber.Ctx) error {
	email, _ := getSession(c)
	if err := service.DeleteTOTP(email); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fiber.ErrNotFound
		}
		return err
	}
	return toJSON(c, fiber.StatusOK)
}

func Auth(c *fiber.Ctx) error {
	if slices.Contains(
		[]string{
			"/api/account/sign-up",
			"/api/account/sign-in",
			"/api/user/profiles",
		},
		c.Path(),
	) {
		return c.Next()
	}
	session, err := store.Get(c)
	if err != nil {
		return err
	}
	if session.Fresh() {
		return fiber.ErrUnauthorized
	}
	email := session.Get(sessionEmail).(string)
	status := session.Get(sessionStatus).(transfer.AccountStatus)
	if status == transfer.AccountSignedIn ||
		(status == transfer.AccountNeedMFA && c.Path() == "/api/account/mfa") ||
		(status == transfer.AccountUnverified &&
			strings.HasPrefix(c.Path(), "/api/account/verification")) {
		return fiber.ErrForbidden
	}
	c.Locals(sessionEmail, email)
	c.Locals(sessionStatus, status)
	return c.Next()
}

func getSession(c *fiber.Ctx) (string, transfer.AccountStatus) {
	return c.Locals(sessionEmail).(string),
		c.Locals(sessionStatus).(transfer.AccountStatus)
}

func setSession(
	c *fiber.Ctx,
	email string,
	status transfer.AccountStatus,
) error {
	session, err := store.Get(c)
	if err != nil {
		return err
	}
	session.Set(sessionEmail, email)
	session.Set(sessionStatus, status)
	return session.Save()
}
