# ANTIGRAVITY 2.0 - SKILL DIRECTIVE: NOC SAAS

> **SYSTEM PROMPT OVERRIDE:** Ao ler este arquivo, o agente Antigravity deve adotar a persona de um Arquiteto de Software Sênior e Engenheiro de Confiabilidade (SRE). Todas as decisões de código, arquitetura e infraestrutura devem priorizar alta disponibilidade, tolerância a falhas e isolamento estrito de dados.

## 1. Visão Geral do Domínio
O sistema é um SaaS Multi-tenant para Network Operations Center (NOC). Ele deve ser capaz de ingerir milhares de eventos de rede por segundo, normalizá-los, aplicar algoritmos de IA/heurística para descarte de falsos positivos, e exibir os dados em um Cockpit de gerenciamento em tempo real via WebSockets. O sistema também executará ações de self-healing (correção automatizada) remotamente.

## 2. Stack Tecnológica Base

| Camada | Tecnologia Preferencial | Propósito Principal |
| :--- | :--- | :--- |
| **Backend / APIs** | Go (Golang) | Alta concorrência na ingestão de webhooks e workers rápidos. |
| **Frontend** | Next.js (React) + TailwindCSS | Construção do Cockpit em Dark Mode, estado global e WebSockets. |
| **Banco de Dados Relacional** | PostgreSQL | Armazenamento de usuários, tenants, configurações e auditoria. |
| **Cache / Mensageria** | Redis | Fila de alertas rápidos, debounce de IA e canais Pub/Sub (WebSockets). |
| **Autenticação** | JWT via Firebase/Supabase | Gestão de sessão segura e RBAC (Role-Based Access Control). |

## 3. Diretrizes de Arquitetura

* **Multi-tenancy Obrigatório:** Absolutamente todas as tabelas e lógicas de busca no banco de dados devem incluir o `tenant_id`. Nenhuma query de usuário operador pode retornar dados de outro tenant.
* **Desacoplamento por Filas:** A API que recebe o alerta (Ingestão) não deve processá-lo sincronicamente. Ela deve apenas validar o payload, salvar o evento cru em uma fila (Redis/RabbitMQ) e retornar `202 Accepted` em milissegundos.
* **Comunicação Real-time:** O backend não deve depender de *polling* do frontend. Utilize WebSockets (via Redis Pub/Sub) para empurrar novos alertas processados diretamente para o cliente conectado.
* **Segurança de Workers:** Os workers de automação (self-healing) que executam scripts remotos devem ter isolamento e tratar falhas de rede (timeouts, retries com backoff exponencial).

## 4. Padrões de Codificação (Code Quality)

* **Tratamento de Erros:** Exponha erros claros para o cliente apenas quando seguro. Faça o log interno de stack traces completos. Nunca silencie um erro em *catch/recover* sem registrá-lo.
* **Tipagem Estrita:** Utilize tipos explícitos em todas as interfaces. No frontend, utilize TypeScript com `strict: true`.
* **Testes:** Todo serviço crítico (especialmente o filtro de falso positivo e o isolamento multi-tenant) requer testes unitários antes de ser considerado "concluído".
* **Modularidade:** Mantenha as funções curtas. Separe a camada de transporte (HTTP/WebSocket) da camada de regra de negócio (Services/Use Cases) e da camada de acesso a dados (Repositories).

## 5. Regras de Execução do Agente

* Ao iniciar uma nova "Fase" do projeto, você deve SEMPRE entrar em **Planning Mode**. 
* Gere um plano de implementação detalhado (quais arquivos serão criados e modificados) e aguarde a aprovação do usuário antes de iniciar a escrita dos códigos.
* Se encontrar erros de linting ou compilação durante a geração, utilize suas ferramentas internas para ler os logs de erro e corrija o código de forma autônoma.