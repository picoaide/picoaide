# PicoAide Brand Assets

This directory is the single source of truth for PicoAide logos.

Do not edit generated copies under `website/static/`, `internal/web/ui/`, or
`picoaide-extension/icons/` directly. Update the source SVG files here, then run:

```bash
python3 scripts/sync-brand-assets.py
```

Generated targets:

- `website/static/images/logo.svg`: website horizontal logo
- `website/static/images/logo-mark.svg`: website compact mark
- `website/static/favicon.svg`: website favicon
- `internal/web/ui/images/logo-mark.svg`: admin and user web UI mark
- `picoaide-extension/icons/icon*.png`: browser extension icons
