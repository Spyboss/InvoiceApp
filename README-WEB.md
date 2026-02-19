# Invoice App - Web Transformation

## Architecture
- **Frontend**: React + TypeScript + Vite + Tailwind CSS
- **Backend**: FastAPI (Python)
- **Database**: Supabase (PostgreSQL)
- **Hosting**: Vercel

## Project Structure
- `api/`: Backend code (FastAPI)
  - `main.py`: Entry point
  - `db.py`: Database connection logic
  - `requirements.txt`: Python dependencies
- `client/`: Frontend code (React)
- `vercel.json`: Vercel deployment configuration

## Setup Instructions

### Prerequisites
- Node.js & npm
- Python 3.9+
- Supabase Account

### Installation
1. Install Frontend Dependencies:
   ```bash
   cd client
   npm install
   ```
2. Install Backend Dependencies:
   ```bash
   pip install -r api/requirements.txt
   ```

### Local Development
1. Start Frontend:
   ```bash
   cd client
   npm run dev
   ```
2. Start Backend:
   ```bash
   uvicorn api.main:app --reload
   ```

### Deployment (Vercel)
1. Install Vercel CLI: `npm i -g vercel`
2. Run `vercel` in the root directory.
3. Set Environment Variables in Vercel Dashboard:
   - `SUPABASE_URL`
   - `SUPABASE_KEY`

## Database Setup (Supabase)
Create a table `invoices` with columns:
- `invoice_no` (text)
- `invoice_type` (text)
- `year` (int)
- `customer` (text)
- ... (other fields)

## Next Steps
- Implement Auth in Frontend (Supabase Auth UI).
- Connect Frontend forms to Backend API.
