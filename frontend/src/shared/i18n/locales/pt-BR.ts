/**
 * pt-BR catalog (default). It is the source of the message schema: the `en` catalog must have
 * exactly the same keys (checked by the compiler and by a unit test). Terms follow the glossary
 * in PROJETO.md section 4.
 */
export const ptBR = {
  app: {
    name: 'Easy Code',
    documentTitle: '{page} · Easy Code',
    skipToContent: 'Pular para o conteúdo',
  },
  navigation: {
    mainLabel: 'Principal',
    toggleMenu: 'Abrir ou fechar o menu',
    home: 'Início',
    projects: 'Projetos',
    pendingItems: 'Pendências',
    agents: 'Agentes e skills',
    settings: 'Configurações',
  },
  language: {
    switcherLabel: 'Idioma da interface: {current}',
    menuLabel: 'Idioma da interface',
    names: {
      'pt-BR': 'Português (Brasil)',
      en: 'English',
    },
  },
  theme: {
    switcherLabel: 'Tema: {current}',
    menuLabel: 'Tema',
    names: {
      system: 'Igual ao sistema',
      light: 'Claro',
      dark: 'Escuro',
    },
  },
  pages: {
    home: {
      title: 'Início',
      description: 'Resumo dos seus projetos, ondas em andamento e pendências.',
    },
    projects: {
      title: 'Projetos',
      description: 'Lista de projetos da organização, com etapa atual, gates e custo.',
    },
    pendingItems: {
      title: 'Pendências',
      description: 'Gates, perguntas de agentes e achados que esperam a sua decisão.',
    },
    agents: {
      title: 'Agentes e skills',
      description: 'Os sete agentes, suas skills, ferramentas e pares de modelos.',
    },
    settings: {
      title: 'Configurações',
      description: 'Provedores, tabela de preços, skills e orçamento da organização.',
    },
    login: {
      title: 'Entrar no Easy Code',
      description: 'O acesso será pelo SSO da sua empresa.',
      signIn: 'Entrar pelo SSO',
      unavailable: 'O login pelo SSO ainda não está disponível nesta versão.',
    },
  },
  placeholder: {
    underConstruction: 'Esta tela está em construção.',
  },
}

export type MessageSchema = typeof ptBR
