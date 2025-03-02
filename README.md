> ‼️ Disclaimer: Built with AI. Do not trust this software. Use at your own risk. ‼️

# ADP backup

Needs Chrome.

```bash
# present your credentials
export ADP_USERNAME=...
export ADP_PASSWORD=...

# backup all PDFs to ~/Downloads/adpworld.adp.com
go run main.go download
# use --headless=false to debug browser automation
go run main.go download --headless=false

# rename all PDFs in ~/Downloads/adpworld.adp.com
go run main.go process
```

