<script>
	import { onMount } from 'svelte';
	import { api } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import { toast } from '#lib/toast.js';
	import Icon from './Icon.svelte';
	import claudeLogo from '#lib/assets/claude-code.svg?url';
	import opencodeLogo from '#lib/assets/opencode.svg?url';
	import piLogo from '#lib/assets/pi.svg?url';
	import codexLogo from '#lib/assets/codex.svg?url';

	let agents = [];
	let models = [];
	let model = '';
	let loading = true;
	let applying = '';
	let disconnecting = '';
	let result = null;

	onMount(load);

	async function load() {
		loading = true;
		try {
			const data = await api.get('/api/agents');
			agents = data.agents || [];
			models = data.models || [];
			const saved = typeof localStorage !== 'undefined' ? localStorage.getItem('flashroute.model') : '';
			model = models.includes(saved) ? saved : models[0] || '';
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			loading = false;
		}
	}

	async function apply(agent) {
		applying = agent.id;
		result = null;
		try {
			result = await api.post('/api/agents/apply', { agent: agent.id, model });
			agents = agents.map((item) => (item.id === agent.id ? { ...item, connected: true } : item));
			toast($t('agents.applied', { agent: agent.name }), 'success');
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			applying = '';
		}
	}

	async function toggleAgent(agent) {
		if (agent.connected) await disconnect(agent);
		else await apply(agent);
	}

	async function disconnect(agent) {
		if (!confirm($t('agents.disconnectConfirm', { agent: agent.name }))) return;
		disconnecting = agent.id;
		result = null;
		try {
			await api.post('/api/agents/disconnect', { agent: agent.id });
			agents = agents.map((item) => (item.id === agent.id ? { ...item, connected: false } : item));
			toast($t('agents.disconnected', { agent: agent.name }), 'success');
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			disconnecting = '';
		}
	}

	const agentLogos = { claude: claudeLogo, opencode: opencodeLogo, pi: piLogo, codex: codexLogo };
</script>

<div class="agents-page">
	<section class="card model-bar">
		<div class="model-bar-icon"><Icon name="plug" size={20} /></div>
		<div class="model-bar-body">
			<span class="eyebrow">{$t('agents.model')}</span>
			<select id="agent-model" class="select" bind:value={model} disabled={loading || !models.length}>
				{#if !models.length}<option value="">{$t('agents.noModels')}</option>{/if}
				{#each models as item (item)}
					<option value={item}>{item}</option>
				{/each}
			</select>
			<span class="hint">{$t('agents.modelHint')}</span>
		</div>
		<div class="model-bar-status">
			<span class="status-dot"></span>
			<span>{agents.filter((agent) => agent.detected).length} / {agents.length}</span>
			<span class="status-label">{$t('agents.detected')}</span>
		</div>
	</section>

	{#if loading}
		<div class="card card-pad"><div class="skeleton" style="width:40%"></div></div>
	{:else}
		<section class="agents-section">
			<div class="agents-section-head">
				<div>
					<h2>{$t('agents.detectedTitle')}</h2>
					<p>{$t('agents.detectedHint')}</p>
				</div>
			</div>
			<div class="agent-grid">
				{#each agents as agent (agent.id)}
					<article class="card agent-card" class:disabled={!agent.detected} class:connected={agent.connected}>
						<div class="agent-top">
							<div class="agent-logo {agent.id}"><img src={agentLogos[agent.id]} alt="" /></div>
							<div class="agent-identity">
								<div class="agent-title-row">
									<h2>{agent.name}</h2>
									{#if agent.connected}
										<span class="badge badge-ok"><Icon name="check" size={11} />{$t('agents.connectedBadge')}</span>
									{:else}
										<span class="badge {agent.detected ? 'badge-ok' : 'badge-off'}">
											{agent.detected ? $t('agents.detected') : $t('agents.notDetected')}
										</span>
									{/if}
								</div>
								<span class="agent-kind">{$t(agent.id === 'claude' ? 'agents.anthropicEndpoint' : 'agents.openaiEndpoint')}</span>
							</div>
							<div class="agent-switch">
								<span class="switch-label">{$t('agents.access')}</span>
								{#if applying === agent.id || disconnecting === agent.id}
									<span class="spinner"></span>
								{/if}
								<button
									class="switch"
									class:on={agent.connected}
									onclick={() => toggleAgent(agent)}
									disabled={!agent.detected || (!agent.connected && !model) || applying === agent.id || disconnecting === agent.id}
									aria-label={agent.connected ? $t('agents.disconnect') : $t('agents.apply')}
									aria-pressed={agent.connected}
								></button>
							</div>
						</div>
						<p class="agent-hint">{$t(agent.id === 'claude' ? 'agents.claudeHint' : agent.id === 'pi' ? 'agents.piHint' : agent.id === 'codex' ? 'agents.codexHint' : 'agents.opencodeHint')}</p>
						<div class="agent-config">
							<span class="config-label">{$t('agents.configFile')}</span>
							<code class="mono">{agent.path}</code>
						</div>
						{#if result?.agent === agent.id}
							<div class="agent-footer">
								<span class="agent-result"><Icon name="check" size={12} />{$t('agents.saved')}</span>
							</div>
						{/if}
					</article>
				{/each}
			</div>
		</section>
	{/if}
</div>

<style>
	.agents-page {
		display: flex;
		flex-direction: column;
		gap: 18px;
	}
	.model-bar {
		display: flex;
		align-items: center;
		gap: 16px;
		padding: 16px 18px;
		background:
			radial-gradient(420px 140px at 0% 0%, var(--accent-soft), transparent 70%),
			var(--card-bg-end);
	}
	.model-bar-icon {
		width: 44px;
		height: 44px;
		border-radius: 13px;
		display: grid;
		place-items: center;
		background: linear-gradient(135deg, var(--accent), var(--accent-strong));
		color: var(--accent-contrast);
		box-shadow: 0 8px 24px var(--accent-shadow);
		flex: none;
	}
	.model-bar-body {
		display: flex;
		flex-direction: column;
		gap: 3px;
		flex: 1;
		min-width: 0;
	}
	.eyebrow {
		font-size: 9px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.model-bar-body .select {
		width: min(420px, 100%);
		font-size: 12.5px;
		padding-top: 7px;
		padding-bottom: 7px;
		background-color: var(--surface-inset);
	}
	.model-bar-status {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 11px;
		color: var(--muted);
		white-space: nowrap;
	}
	.status-dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: var(--accent);
		box-shadow: 0 0 0 4px var(--accent-ring);
	}
	.status-label {
		color: var(--text-soft);
	}
	.agents-section {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.agents-section-head h2 {
		font-size: 13px;
		font-weight: 700;
	}
	.agents-section-head p {
		font-size: 11px;
		color: var(--muted);
		margin-top: 3px;
	}
	.agent-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(290px, 1fr));
		gap: 14px;
	}
	.agent-card {
		display: flex;
		flex-direction: column;
		gap: 11px;
		padding: 16px;
		transition:
			border-color 0.16s ease,
			box-shadow 0.16s ease,
			transform 0.16s ease;
	}
	.agent-card:not(.disabled):hover {
		border-color: var(--accent-border-soft);
		box-shadow: 0 12px 36px var(--surface-tint);
		transform: translateY(-1px);
	}
	.agent-card.disabled {
		opacity: 0.62;
	}
	.agent-card.connected {
		border-color: var(--accent-border);
		background:
			linear-gradient(180deg, var(--accent-soft), transparent 42%),
			var(--card-bg-end);
	}
	.agent-top {
		display: flex;
		align-items: center;
		gap: 11px;
	}
	.agent-logo {
		width: 42px;
		height: 42px;
		border-radius: 12px;
		display: grid;
		place-items: center;
		background: #ffffff;
		border: 1px solid var(--line-soft);
		flex: none;
		box-shadow: 0 6px 18px rgba(0, 0, 0, 0.08);
		padding: 8px;
	}
	.agent-logo img {
		width: 26px;
		height: 26px;
		object-fit: contain;
		display: block;
	}
	.agent-identity {
		display: flex;
		flex: 1;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
	.agent-title-row {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.agent-title-row h2 {
		font-size: 13.5px;
		font-weight: 700;
	}
	.agent-kind {
		font-size: 10px;
		color: var(--muted);
	}
	.agent-hint {
		font-size: 11.5px;
		color: var(--muted);
		line-height: 1.55;
		min-height: 36px;
	}
	.agent-config {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 9px 10px;
		border: 1px solid var(--line-soft);
		border-radius: 9px;
		background: var(--surface-inset);
	}
	.config-label {
		font-size: 9px;
		font-weight: 700;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.agent-config code {
		font-size: 10.5px;
		color: var(--text-soft);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.agent-footer {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		margin-top: auto;
	}
	.agent-result {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		font-size: 10.5px;
		color: var(--accent);
	}
	.agent-switch {
		display: flex;
		align-items: center;
		gap: 8px;
		flex: none;
	}
	.agent-switch .switch-label {
		font-size: 11px;
		font-weight: 600;
		color: var(--muted);
	}
	@media (max-width: 640px) {
		.model-bar {
			align-items: flex-start;
			flex-wrap: wrap;
		}
		.model-bar-status {
			margin-left: 60px;
		}
	}
</style>
