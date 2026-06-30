# Guia Simplificado: Como Colocar seu NOC/SOC no Ar na Railway (Para Iniciantes) ☁️

Se você nunca colocou um site ou sistema na nuvem antes, não se preocupe! Este guia foi feito para ser o mais simples possível, explicando passo a passo onde clicar e o que fazer para colocar o sistema inteiro no ar em minutos.

---

## 🛠️ O que você precisa antes de começar:
1.  Ter uma conta no **GitHub** (onde seu código já está salvo).
2.  Ter uma conta na **[Railway](https://railway.app/)** (você pode entrar clicando em "Log in" e selecionando "Sign in with GitHub").

---

## 🚀 Passo 1: Criar o Projeto na Railway
Ao entrar na Railway, você verá sua tela de projetos (Dashboard).
1.  Clique no botão grande **"New Project"** (Novo Projeto).
2.  Na listinha que aparecer, selecione **"Provision PostgreSQL"** (isso criará o seu banco de dados principal de forma automática).
3.  Aguarde alguns segundos até ver um quadradinho vermelho/azul escrito **"PostgreSQL"** na sua tela.

---

## 🚀 Passo 2: Adicionar o Redis
O Redis serve como a fila que organiza os alertas em tempo real.
1.  No painel do seu projeto, clique no botão **"New"** (no canto superior direito).
2.  Selecione **"Database"** e depois **"Add Redis"**.
3.  Um novo quadradinho vermelho escrito **"Redis"** aparecerá na sua tela.

---

## 🚀-% Passo 3: Colocar o Servidor Go no Ar (Backend)
Esta é a "central de inteligência" que recebe os alertas dos aplicativos.
1.  Clique no botão **"New"** (superior direito) > selecione **"GitHub Repo"**.
2.  Selecione o seu repositório do GitHub (ex: `cevsouza/noc-soc-saas`).
3.  Aguarde a criação do bloco. Clique em cima dele e vá na aba **"Variables"** (Variáveis).
4.  Adicione as seguintes variáveis de ambiente clicando em **"New Variable"**:
    *   **`DATABASE_URL`**: Digite `${{Postgres.DATABASE_URL}}` (isso conecta o servidor ao banco de dados).
    *   **`REDIS_URL`**: Digite `${{Redis.REDIS_URL}}` (isso conecta o servidor ao Redis).
    *   **`PORT`**: Digite `8080` (a porta do servidor).
    *   **`VAULT_MASTER_KEY`**: Digite qualquer texto aleatório secreto de 32 letras/números (ex: `12345678901234567890123456789012`). *Guarde este texto em local seguro, ele criptografa as chaves de acesso!*
5.  Agora vá na aba **"Settings"** (Configurações) deste bloco:
    *   Desça a página até achar a seção **"Public Networking"**.
    *   Clique em **"Generate Domain"** (Gerar Domínio). Isso criará um endereço de internet público para a sua API (ex: `https://noc-soc-saas-production.up.railway.app`). **Copie esse link!**

> [!NOTE]
> Ao iniciar o Servidor Go pela primeira vez, ele criará de forma 100% automática todas as tabelas do banco de dados e as políticas de segurança. Não é necessário rodar nenhuma query SQL manual!

---

## 🚀 Passo 4: Colocar o Robô de IA no Ar (Python Worker)
Este robô processa os alertas em segundo plano para limpar alertas duplicados e ruídos.
1.  Clique em **"New"** (superior direito) > **"GitHub Repo"** > selecione o mesmo repositório do GitHub.
2.  O Railway criará um segundo bloco. Clique nele, vá em **"Settings"** e mude o nome dele para `ai-worker`.
3.  Ainda na aba **"Settings"**, role até a opção **"Start Command"** (Comando de Inicialização) e digite exatamente:
    ```bash
    python workers/ai_worker.py
    ```
4.  Vá na aba **"Variables"** deste serviço e adicione exatamente as mesmas variáveis que colocou no passo anterior:
    *   `DATABASE_URL` contendo `${{Postgres.DATABASE_URL}}`
    *   `REDIS_URL` contendo `${{Redis.REDIS_URL}}`
    *   `VAULT_MASTER_KEY` contendo o mesmo texto secreto de 32 letras.
5.  *Dica:* Este serviço não precisa de domínio público. Deixe ele sem link! Ele rodará silenciosamente no fundo.

---

## 🚀 Passo 5: Colocar a Interface do Operador no Ar (Cockpit Next.js)
Esta é a tela preta bonita (dashboard) onde os operadores visualizam os alertas do NOC.
1.  Clique em **"New"** (superior direito) > **"GitHub Repo"** > selecione o repositório novamente.
2.  Clique no novo bloco que surgiu, vá em **"Settings"** e altere o nome para `cockpit-ui`.
3.  Ainda em **"Settings"**, ache a opção **"Root Directory"** (Diretório Raiz) e mude para:
    `frontend`
4.  Vá na aba **"Variables"** e adicione:
    *   **`NEXT_PUBLIC_API_URL`**: Cole a URL pública da sua Go API gerada no **Passo 3** (ex: `https://noc-soc-saas-production.up.railway.app`).
5.  Vá em **"Settings"** > desça até **"Public Networking"** > clique em **"Generate Domain"** para obter o link público do Cockpit.
6.  Aguarde o Railway terminar a compilação (você pode ver o status clicando no serviço).

🎉 **Pronto!** Clicando no link gerado para o `cockpit-ui`, o seu sistema estará online na nuvem, pronto para ser usado e compartilhado com qualquer pessoa no mundo!
