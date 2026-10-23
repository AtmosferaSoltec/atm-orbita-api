# Plantillas de correo

Las fuentes son los `.mjml`; los `.html` son su compilación y son los que embebe el API (`go:embed`), completados con `html/template` (`{{.Code}}`, `{{.Minutes}}`). Así la imagen no necesita Node.

Después de cambiar un `.mjml`, vuelve a compilar desde la raíz del repo (PowerShell):

```powershell
docker run --rm -v "${PWD}\internal\integrations\mail\templates:/t" -w /t node:22-alpine sh -c "npx -y mjml@4.15.3 reset_code.mjml -o reset_code.html && npx -y mjml@4.15.3 password_changed.mjml -o password_changed.html"
```
