# 📄 Invoice / Proforma Generator

A lightweight web application for generating **Sales Invoices**, **Leasing Invoices**, **Proforma Invoices**, and **Advance Payment Receipts** for dealerships.

**Live Web App:** https://your-app.streamlit.app

---

## 🚀 Features

* Generate **Sales-Cash**, **Sales-Leasing**, **Proforma**, and **Advance Payment** receipts
* Professional PDF generation with company logos
* Automatic **invoice number tracking per year**
* Save all invoice details to CSV
* Download PDFs instantly
* Works on any device via browser
* Fully offline capable - no database needed

---

## 🖥️ Running Locally

### Web Version (Streamlit)
```bash
pip install -r requirements.txt
streamlit run web_app.py
```

### Desktop Version (Tkinter)
```bash
pip install reportlab
python invoice_app.py
```

---

## ☁️ Deploy to Streamlit Cloud (Free)

1. **Push to GitHub**
   ```bash
   git init
   git add .
   git commit -m "Initial commit"
   git branch -M main
   git remote add origin https://github.com/YOUR_USERNAME/invoice-app.git
   git push -u origin main
   ```

2. **Deploy**
   - Go to [streamlit.io](https://streamlit.io/cloud)
   - Connect your GitHub account
   - Click "New App" → Select your repo
   - Set: Main file path = `web_app.py`
   - Click "Deploy!"

3. **Your app will be live at:** `https://your-app-name.streamlit.app`

---

## 📁 Project Structure

```
/
├─ web_app.py           # Streamlit web application
├─ invoice_app.py        # Desktop Tkinter application
├─ requirements.txt      # Python dependencies
├─ download.png         # Company logo
├─ singer_logo.png      # Singer logo for proforma
├─ .streamlit/
│   └─ config.toml      # Streamlit settings
└─ .gitignore
```

---

## 📝 License

This project is free to use and modify.

