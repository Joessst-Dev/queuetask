import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'queuetask',
  description: 'Workflow orchestration backed by PostgreSQL',
  base: '/queuetask/',
  ignoreDeadLinks: [/^http:\/\/localhost/],

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Reference', link: '/reference/workflow' },
      { text: 'GitHub', link: 'https://github.com/Joessst-Dev/queuetask' },
    ],

    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Getting Started', link: '/guide/getting-started' },
          { text: 'How It Works',    link: '/guide/how-it-works' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'Workflow YAML',   link: '/reference/workflow' },
          { text: 'REST API',        link: '/reference/api' },
          { text: 'Configuration',   link: '/reference/configuration' },
          { text: 'Notifications',   link: '/reference/notifications' },
        ],
      },
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/Joessst-Dev/queuetask' },
    ],

    search: { provider: 'local' },
  },
})
