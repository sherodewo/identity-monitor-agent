package dashboard

import "github.com/gofiber/fiber/v2"

func (s *Server) executions(c *fiber.Ctx) error {
	rows, err := s.Store.Executions(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": rows})
}
func (s *Server) clearData(c *fiber.Ctx) error {
	var request struct {
		Execution    string `json:"execution"`
		Confirmation string `json:"confirmation"`
	}
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(400, "invalid request")
	}
	if request.Confirmation != "HAPUS" {
		return fiber.NewError(400, "ketik HAPUS untuk konfirmasi")
	}
	n, err := s.Supervisor.ClearData(c.UserContext(), request.Execution)
	if err != nil {
		return fiber.NewError(409, err.Error())
	}
	return c.JSON(fiber.Map{"deleted": n})
}
