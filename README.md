# ConnectAI Hub 🚀

### One Hub. Endless Possibilities.

ConnectAI Hub is an all-in-one AI-powered platform that brings multiple intelligent tools together in one workspace. It helps users learn, build, analyse information, manage business tasks and improve productivity using Google's Gemini AI.

Instead of switching between different AI platforms for different tasks, users can access seven specialised AI assistants from a single account.

## ✨ Features

ConnectAI Hub currently provides seven specialised AI modules:

### 1. 📚 Study AI
Your personal learning companion.

- Understand difficult topics through simple explanations.
- Generate practice questions.
- Summarise learning materials.
- Learn concepts step by step.

### 2. 💼 Career AI
Your career development assistant.

- Improve CV content.
- Practise interview questions.
- Explore career paths.
- Develop professional career materials.

### 3. 🏢 Business AI
Your business planning assistant.

- Develop business ideas.
- Create marketing plans.
- Write product descriptions.
- Explore practical business strategies.

### 4. 📄 Document AI
Your document analysis assistant.

- Upload and analyse documents.
- Summarise important information.
- Extract key details.
- Ask questions about uploaded files.

Supported formats:
- PDF
- DOCX
- TXT
- Markdown
- CSV
- JSON
- LOG

### 5. 🌐 Website AI
Your website planning assistant.

- Plan website structures.
- Organise website pages.
- Generate website content ideas.
- Explore interface and usability improvements.

### 6. 🎧 Support AI
Your customer support assistant.

- Draft customer support replies.
- Create frequently asked questions.
- Organise support workflows.
- Develop customer service processes.

### 7. 📊 Data AI
Your data analysis assistant.

- Upload and analyse datasets.
- Understand data.
- Identify useful metrics.
- Recommend charts and reports.

Supported formats include CSV, XLSX, TXT, Markdown, JSON and LOG.

## 🔐 Account Features

- Unique email address per account; registered email cannot be edited.
- Password policy: at least 8 characters with a letter, number and symbol.
- Show/hide password controls and live registration checks.
- Email-based password reset links expire after 30 minutes.
- Account deletion requires password confirmation.
- Configure `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` and `APP_BASE_URL` in the deployment environment to enable password-reset email delivery.



- User registration and login.
- Secure password hashing.
- Session-based authentication.
- Protected AI workspaces.
- Conversation history stored in PostgreSQL.
- Clear conversation functionality.
- CSRF protection.

## 🛠️ Technology Stack

**Backend**
- Go (Golang)
- HTTP ServeMux
- PostgreSQL

**Frontend**
- HTML
- CSS
- Vanilla JavaScript
- Go HTML templates

**AI Integration**
- Google Gemini API

**Database Hosting**
- Neon PostgreSQL

**Application Hosting**
- Render

## 🏗️ Project Structure

```text
ConnectAI-Hub/
│
├── cmd/
│
├── internal/
│   ├── ai/
│   │   └── gemini.go
│   │
│   ├── auth/
│   │
│   └── web/
│       ├── server.go
│       └── office_extract.go
│
├── templates/
│   ├── home.html
│   ├── dashboard.html
│   ├── module.html
│   ├── login.html
│   └── register.html
│
├── static/
│   ├── css/
│   ├── js/
│   └── images/
│
├── .env
├── .gitignore
├── go.mod
├── go.sum
└── main.go
```

*The structure above highlights the main application components; individual files may vary as development continues.*

## ⚙️ Getting Started

### Requirements

- Go installed.
- A PostgreSQL database.
- A Google Gemini API key.
- Git.

### 1. Clone the repository

```bash
git clone https://github.com/OmaleGrace/ConnectAI-Hub.git
```

### 2. Enter the project directory

```bash
cd ConnectAI-Hub
```


### 5. Start the application

```bash
go run .
```

Open your browser and visit:

```text
http://localhost:8080
```

## 🗄️ Database

ConnectAI Hub uses PostgreSQL to manage:

- User accounts.
- Authentication sessions.
- AI conversation history.
- Module-specific conversations.
- AI usage activity records, linked to each user account.

The application connects to the configured PostgreSQL database and manages its required tables through its database setup process.

## 🔒 Privacy and Security

- Passwords are hashed before storage.
- Authentication sessions are managed using secure tokens.
- API credentials are stored in environment variables.
- Uploaded documents are processed in memory rather than permanently stored by the application.
- Uploaded documents are sent to Google Gemini for analysis. Avoid uploading sensitive or confidential information.

## 👤 Account Settings and Usage (Batch 7)

- Update your account name and email address.
- Change your password after verifying the current password.
- Changing a password revokes existing sessions and signs the current user back in.
- Dashboard and account settings show all-time and current-month AI request counts.
- Successful chat and file-analysis requests are recorded in `ai_usage`.
- Usage counts represent successful saved requests; they are not billing amounts or token estimates.

The application creates the `ai_usage` table automatically during database startup. The matching SQL migration is also available at `migrations/002_ai_usage.sql`.

## 🚧 Future Improvements

Potential areas for future development include:

- More document formats and advanced file processing.
- AI-generated reports and exports.
- Additional productivity tools.
- Subscription and payment functionality.

## 🎯 Project Vision

The goal of ConnectAI Hub is to make useful AI capabilities accessible through one organised platform.

Whether someone wants to learn a new concept, improve their career materials, develop a business idea, analyse documents, plan a website or understand a dataset, ConnectAI Hub provides a dedicated workspace for the task.

**One account. Seven specialised AI assistants. Endless possibilities.**

## 👩‍💻 Developer

**Omale Grace**

Aspiring Full-Stack Developer | Go Developer | Backend Engineering Enthusiast

GitHub: [@OmaleGrace](https://github.com/OmaleGrace)

---

⭐ If you find this project interesting, consider giving the repository a star!

## Appearance and installable app (Batch 10)

- Users can select Dark, Light, or Follow system in Account settings. The preference is stored locally in their browser/device.
- The app includes a web app manifest, icons, and a service worker for static asset caching. On supported browsers (including Chrome on Android), users can install ConnectAI Hub from the browser menu or the in-app Install app button.
- Serve the app over HTTPS for installation; Render's HTTPS domain is suitable. The PWA install flow creates an app-like installation on the device. It does not produce a standalone downloadable `.apk` file. A signed Android APK requires a Trusted Web Activity/Android build and signing process.
