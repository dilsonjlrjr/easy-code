# Projeto de referência — Reserva de Salas

Necessidade de negócio usada para testar os agentes ponta a ponta (regressão) sempre que
instruções, templates ou skills mudarem. Pequena, mas completa: login, papéis, regra de
negócio sob concorrência (conflito de horário) e controle de acesso por recurso.

O texto abaixo é a entrada entregue ao Engenheiro de Requisitos. Não editar: mudar a entrada
invalida a comparação com execuções anteriores.

---

Sistema web para reservar salas de reunião da empresa. Funcionários entram com login, veem
as salas disponíveis por data e horário, fazem reservas e cancelam as próprias. Não pode
haver duas reservas da mesma sala no mesmo horário. Administradores cadastram salas (nome,
capacidade, recursos como projetor e videoconferência) e podem cancelar qualquer reserva.
Cada funcionário vê a lista das suas reservas futuras.
