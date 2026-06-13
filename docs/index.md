---
layout: home

hero:
  name: queuetask
  text: Workflow orchestration backed by PostgreSQL
  tagline: Define step sequences in YAML. The engine tracks state, activates steps when dependencies complete, and notifies your team.
  actions:
    - theme: brand
      text: Get Started
      link: /guide/getting-started
    - theme: alt
      text: GitHub
      link: https://github.com/Joessst-Dev/queuetask

features:
  - title: Four step trigger types
    details: manual, auto, http, and queueti — mix them freely in a single workflow.
  - title: Dependency-driven execution
    details: Steps activate automatically when all depends_on steps complete. Outputs merge and flow forward as the next step's input.
  - title: Email & SMS notifications
    details: Fire alerts on instance.completed, instance.failed, or step.waiting_manual via SMTP, SendGrid, Mailgun, Twilio, or Vonage.
  - title: Live UI + REST API
    details: Manage instances from the browser or curl. Hot-reload workflows without restarting the server.
---
