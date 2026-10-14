package dashboard

import "github.com/gofiber/fiber/v2"

func (s *Server) reviewRelevance(c *fiber.Ctx) error {
	mentionID, err := id(c)
	if err != nil {
		return err
	}
	var request struct {
		Decision string `json:"decision"`
	}
	if err = c.BodyParser(&request); err != nil {
		return fiber.NewError(400, "invalid relevance decision")
	}
	if request.Decision != "relevant" && request.Decision != "excluded" {
		return fiber.NewError(400, "decision must be relevant or excluded")
	}
	if err = s.Store.ReviewRelevance(c.UserContext(), mentionID, request.Decision, "local-reviewer"); err != nil {
		return err
	}
	m, err := s.Store.Mention(c.UserContext(), mentionID)
	if err != nil {
		return err
	}
	return c.JSON(m)
}

func relevanceFilter(c *fiber.Ctx) (string, error) {
	v := c.Query("relevance", "active")
	switch v {
	case "active", "relevant", "pending", "excluded", "all":
		return v, nil
	}
	return "", fiber.NewError(400, "relevance must be active, relevant, pending, excluded or all")
}
