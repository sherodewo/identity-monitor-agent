package dashboard

import "github.com/gofiber/fiber/v2"

func (s *Server) startBatch(c *fiber.Ctx) error {
	var request struct {
		Subject string   `json:"subject_id"`
		Source  string   `json:"source"`
		Queries []string `json:"queries"`
		From    string   `json:"date_from"`
		To      string   `json:"date_to"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(400, "invalid batch JSON")
	}
	batch, err := s.Supervisor.SubmitBatch(c.UserContext(), request.Subject, request.Source, request.Queries, request.From, request.To)
	if err != nil {
		return fiber.NewError(409, err.Error())
	}
	return c.Status(202).JSON(batch)
}

func (s *Server) progress(c *fiber.Ctx) error {
	data, err := s.Store.ExecutionProgress(c.UserContext(), c.Query("execution"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"batch": s.Supervisor.Batch(), "data": data})
}
