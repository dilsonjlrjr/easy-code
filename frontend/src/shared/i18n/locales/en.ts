import type { MessageSchema } from './pt-BR'

export const en: MessageSchema = {
  app: {
    name: 'Easy Code',
    documentTitle: '{page} · Easy Code',
    skipToContent: 'Skip to content',
  },
  navigation: {
    mainLabel: 'Main',
    toggleMenu: 'Open or close the menu',
    home: 'Home',
    projects: 'Projects',
    pendingItems: 'Pending items',
    agents: 'Agents and skills',
    settings: 'Settings',
  },
  language: {
    switcherLabel: 'Interface language: {current}',
    menuLabel: 'Interface language',
    names: {
      'pt-BR': 'Português (Brasil)',
      en: 'English',
    },
  },
  theme: {
    switcherLabel: 'Theme: {current}',
    menuLabel: 'Theme',
    names: {
      system: 'Same as system',
      light: 'Light',
      dark: 'Dark',
    },
  },
  pages: {
    home: {
      title: 'Home',
      description: 'Summary of your projects, running waves and pending items.',
    },
    projects: {
      title: 'Projects',
      description: "The organization's projects, with current stage, gates and cost.",
    },
    pendingItems: {
      title: 'Pending items',
      description: 'Gates, agent questions and findings waiting for your decision.',
    },
    agents: {
      title: 'Agents and skills',
      description: 'The seven agents, their skills, tools and model pairs.',
    },
    settings: {
      title: 'Settings',
      description: "The organization's providers, price table, skills and budget.",
    },
    login: {
      title: 'Sign in to Easy Code',
      description: "Access will be through your company's SSO.",
      signIn: 'Sign in with SSO',
      unavailable: 'SSO sign-in is not available in this version yet.',
    },
  },
  placeholder: {
    underConstruction: 'This screen is under construction.',
  },
}
