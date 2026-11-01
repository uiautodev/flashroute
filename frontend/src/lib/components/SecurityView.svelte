<script>
	import { onMount } from 'svelte';
	import { api, formatTime } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import { toast } from '#lib/toast.js';
	import Icon from './Icon.svelte';
	import Modal from './Modal.svelte';

	let enabled = true;
	let rules = [];
	let loading = true;
	let saving = false;
	let testText = '';
	let testOutput = '';
	let testing = false;
	let resetting = false;
	let counts = {};
	let recordsOpen = false;
	let activeRule = null;
	let records = [];
	let recordsLoading = false;
	let expandedGroups = {};

	$: totalHits = Object.values(counts).reduce((sum, count) => sum + Number(count || 0), 0);

	$: groupedRules = groupRules(rules);
	$: groupTotals = Object.fromEntries(groupedRules.map((group) => [group.name, group.rules.reduce((sum, rule) => sum + (counts[rule.id] || 0), 0)]));

	onMount(load);

	async function load() {
		loading = true;
		try {
			const data = await api.get('/api/redact');
			enabled = data.enabled !== false;
			rules = data.rules || [];
			counts = data.counts || {};
			expandedGroups = {};
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			loading = false;
		}
	}

	function groupRules(list) {
		const groups = new Map();
		for (const rule of list) {
			const name = (rule.group || '').trim();
			if (!groups.has(name)) groups.set(name, []);
			groups.get(name).push(rule);
		}
		const order = ['keys', 'identity'];
		return [...groups.entries()]
			.sort(([a], [b]) => {
				const ai = order.indexOf(a);
				const bi = order.indexOf(b);
				if (ai !== -1 || bi !== -1) return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi);
				if (a === '') return 1;
				if (b === '') return -1;
				return a.localeCompare(b);
			})
			.map(([name, items]) => ({ name, rules: items }));
	}

	function groupLabel(group) {
		if (group === 'keys' || group === 'identity') return $t('security.group.' + group);
		return group || $t('security.group.other');
	}

	function addRule(group = '') {
		rules = [...rules, { id: '', name: $t('security.redactNewName'), group, pattern: '', enabled: true }];
		expandedGroups = { ...expandedGroups, [group]: true };
	}

	function toggleGroup(group) {
		expandedGroups = { ...expandedGroups, [group]: !expandedGroups[group] };
	}

	function removeRule(rule) {
		rules = rules.filter((item) => item !== rule);
	}

	async function save() {
		saving = true;
		try {
			const data = await api.post('/api/redact', { enabled, rules });
			enabled = data.enabled !== false;
			rules = data.rules || [];
			counts = data.counts || counts;
			toast($t('security.saved'), 'success');
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			saving = false;
		}
	}

	async function resetDefaults() {
		if (!confirm($t('security.redactResetConfirm'))) return;
		resetting = true;
		try {
			const data = await api.post('/api/redact/reset', {});
			enabled = data.enabled !== false;
			rules = data.rules || [];
			counts = data.counts || counts;
			testOutput = '';
			toast($t('security.redactResetDone'), 'success');
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			resetting = false;
		}
	}

	async function openRecords(rule) {
		activeRule = rule;
		recordsOpen = true;
		recordsLoading = true;
		records = [];
		try {
			const data = await api.get('/api/redact/records?rule_id=' + encodeURIComponent(rule.id));
			records = data.records || [];
			counts = data.counts || counts;
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			recordsLoading = false;
		}
	}

	async function clearRecords() {
		if (!confirm($t('security.redactClearConfirm'))) return;
		try {
			await api.del('/api/redact/records');
			records = [];
			counts = {};
			toast($t('security.redactCleared'), 'success');
		} catch (err) {
			toast(err.message, 'error');
		}
	}

	async function testRules() {
		testing = true;
		try {
			const data = await api.post('/api/redact/test', { text: testText, rules });
			testOutput = data.output ?? '';
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			testing = false;
		}
	}
</script>

<section class="card card-pad">
	<div class="section-head">
		<div class="section-icon"><Icon name="shield" size={17} /></div>
		<div class="section-head-text">
			<h2>{$t('security.redactTitle')}</h2>
			<p>{$t('security.redactHint')}</p>
		</div>
		<span class="total-hits">{$t('security.totalHits')} {totalHits}</span>
	</div>
	<div class="divider"></div>
	<div class="redact-switch">
		<button
			class="switch"
			class:on={enabled}
			onclick={() => (enabled = !enabled)}
			aria-label={$t('security.enabledLabel')}
			aria-pressed={enabled}
		></button>
		<div>
			<span class="switch-label">{$t('security.enabledLabel')}</span>
			<span class="hint">{$t('security.enabledHint')}</span>
		</div>
	</div>
	{#if loading}
		<div class="redact-list">
			<div class="skeleton" style="height:34px"></div>
			<div class="skeleton" style="height:34px"></div>
		</div>
	{:else}
		<datalist id="redact-groups">
			<option value="keys">{$t('security.group.keys')}</option>
			<option value="identity">{$t('security.group.identity')}</option>
		</datalist>
		<div class="redact-groups">
			{#each groupedRules as group}
				<div class="redact-group">
					<div class="redact-group-head">
						<button class="group-toggle" onclick={() => toggleGroup(group.name)} aria-expanded={!!expandedGroups[group.name]}>
							<Icon name={expandedGroups[group.name] ? 'chevron-down' : 'chevron-right'} size={13} />
							<span>{groupLabel(group.name)}</span>
							<span class="group-hits">{$t('security.redactHits')} {groupTotals[group.name] || 0}</span>
						</button>
						<button class="btn btn-ghost btn-icon btn-sm" onclick={() => addRule(group.name)} title={$t('security.redactAdd')} aria-label={$t('security.redactAdd')}>
							<Icon name="plus" size={12} />
						</button>
					</div>
					{#if expandedGroups[group.name]}
						<div class="redact-list">
							{#each group.rules as rule (rule.id || rule)}
								<div class="redact-item">
									<input class="rule-name" bind:value={rule.name} placeholder={$t('security.redactName')} aria-label={$t('security.redactName')} />
									<input class="input mono rule-pattern" bind:value={rule.pattern} placeholder={$t('security.redactPattern')} aria-label={$t('security.redactPattern')} />
									<input class="input rule-group" bind:value={rule.group} list="redact-groups" placeholder={$t('security.group')} aria-label={$t('security.group')} onchange={() => (rules = [...rules])} />
									<button class="hits-btn" onclick={() => openRecords(rule)} title={`${$t('security.redactHits')} ${counts[rule.id] || 0}`} aria-label={`${$t('security.redactHits')} ${counts[rule.id] || 0}`}>
										{counts[rule.id] || 0}
									</button>
									<button class="switch small" class:on={rule.enabled} onclick={() => (rule.enabled = !rule.enabled)} title={$t('common.enabled')} aria-label={$t('common.enabled')} aria-pressed={rule.enabled}></button>
									<button class="btn btn-ghost btn-icon" onclick={() => removeRule(rule)} title={$t('common.delete')} aria-label={$t('common.delete')}>
										<Icon name="trash" size={14} />
									</button>
								</div>
							{/each}
						</div>
					{/if}
				</div>
			{:else}
				<span class="hint">{$t('security.redactEmpty')}</span>
			{/each}
		</div>
		<div class="redact-actions">
			<button class="btn" onclick={() => addRule()}><Icon name="plus" size={14} />{$t('security.redactAdd')}</button>
			<button class="btn" onclick={resetDefaults} disabled={resetting}>
				{#if resetting}<span class="spinner"></span>{:else}<Icon name="refresh" size={14} />{/if}
				{$t('security.redactReset')}
			</button>
			<button class="btn btn-primary" onclick={save} disabled={saving}>
				{#if saving}<span class="spinner"></span>{/if}
				{$t('common.save')}
			</button>
		</div>
		<div class="divider"></div>
		<div class="test-block">
			<label for="redact-test" class="field-label">{$t('security.testLabel')}</label>
			<textarea
				id="redact-test"
				class="textarea mono"
				bind:value={testText}
				placeholder={$t('security.testPlaceholder')}
			></textarea>
			<div class="test-actions">
				<button class="btn" onclick={testRules} disabled={testing}>
					{#if testing}<span class="spinner"></span>{/if}
					{$t('security.testRun')}
				</button>
			</div>
			{#if testOutput}
				<div class="field">
					<span class="field-label">{$t('security.testResult')}</span>
					<div class="code-block test-output">{testOutput}</div>
				</div>
			{/if}
		</div>
	{/if}
</section>

{#if recordsOpen}
	<Modal title={$t('security.redactRecords')} onClose={() => (recordsOpen = false)} closeOnBackdrop>
		<div class="records-head">
			<span class="hint">{$t('security.redactRetentionHint')}</span>
			<button class="btn btn-sm btn-danger" onclick={clearRecords} disabled={!records.length}>
				<Icon name="trash" size={13} />{$t('security.redactClearRecords')}
			</button>
		</div>
		{#if recordsLoading}
			<div class="row" style="justify-content:center;padding:18px"><span class="spinner"></span></div>
		{:else if records.length === 0}
			<p class="hint">{$t('security.redactRecordsEmpty')}</p>
		{:else}
			<div class="record-list">
				{#each records as record}
					<div class="record-row">
						<div class="record-main">
							<code>{record.original}</code>
							<span>→</span>
							<code class="record-replacement">{record.replacement}</code>
						</div>
						<div class="record-side">
							{#if record.count > 1}<span class="record-count">×{record.count}</span>{/if}
							<span class="record-time">{formatTime(record.created_at)}</span>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</Modal>
{/if}

<style>
	.section-head {
		display: flex;
		gap: 11px;
		align-items: flex-start;
	}
	.section-head-text {
		flex: 1;
		min-width: 0;
	}
	.total-hits {
		flex: none;
		padding: 4px 9px;
		border-radius: 999px;
		background: var(--accent-soft);
		color: var(--accent);
		font-size: 10.5px;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	.section-icon {
		width: 34px;
		height: 34px;
		border-radius: 10px;
		display: grid;
		place-items: center;
		background: var(--accent-soft);
		color: var(--accent);
		border: 1px solid var(--accent-border-soft);
		flex: none;
	}
	.section-head h2 {
		font-size: 14px;
		font-weight: 680;
	}
	.section-head p {
		font-size: 11.5px;
		color: var(--muted);
		line-height: 1.55;
		margin-top: 4px;
	}
	.redact-switch {
		display: flex;
		align-items: center;
		gap: 10px;
		margin-bottom: 14px;
	}
	.redact-switch > div {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.switch-label {
		font-size: 12px;
		font-weight: 650;
		color: var(--text-soft);
	}
	.redact-groups {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.redact-group {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.redact-group-head {
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: 11px;
		font-weight: 700;
		letter-spacing: 0.04em;
		color: var(--text-soft);
	}
	.group-toggle {
		display: flex;
		align-items: center;
		gap: 6px;
		flex: 1;
		min-width: 0;
		padding: 4px 2px;
		border: 0;
		background: transparent;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.group-toggle:hover {
		color: var(--accent);
	}
	.group-hits {
		margin-left: auto;
		padding: 2px 7px;
		border-radius: 999px;
		background: var(--accent-soft);
		color: var(--accent);
		font-size: 10px;
		font-weight: 650;
		font-variant-numeric: tabular-nums;
	}
	.redact-list {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.redact-item {
		display: flex;
		align-items: center;
		gap: 6px;
		flex-wrap: wrap;
		padding: 6px 8px;
		border: 1px solid var(--line-soft);
		border-radius: 9px;
		background: var(--surface-inset);
	}
	.rule-name {
		flex: 1 1 120px;
		min-width: 100px;
		height: 28px;
		padding: 3px 1px;
		border: 0;
		border-bottom: 1px solid transparent;
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: 11.5px;
		font-weight: 650;
	}
	.rule-name:hover {
		border-bottom-color: var(--line);
	}
	.rule-name:focus {
		outline: none;
		border-bottom-color: var(--accent);
	}
	.rule-pattern {
		flex: 3 1 260px;
	}
	.rule-group {
		flex: 0 1 84px;
		min-width: 70px;
	}
	.redact-item .input {
		height: 28px;
		padding: 3px 7px;
		font-size: 10.5px;
	}
	.hits-btn {
		min-width: 28px;
		padding: 4px 7px;
		border: 1px solid var(--line);
		border-radius: 7px;
		background: var(--surface-inset);
		color: var(--muted);
		font-size: 10.5px;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	.hits-btn:hover {
		border-color: var(--accent);
		color: var(--accent);
	}
	.records-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		margin-bottom: 12px;
	}
	.record-list {
		display: flex;
		flex-direction: column;
		gap: 6px;
		max-height: 360px;
		overflow-y: auto;
	}
	.record-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		padding: 8px 10px;
		border: 1px solid var(--line-soft);
		border-radius: 8px;
		background: var(--surface-inset);
	}
	.record-main {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
		overflow-wrap: anywhere;
		font-size: 11px;
	}
	.record-main code {
		font-size: 10.5px;
		overflow-wrap: anywhere;
	}
	.record-replacement {
		color: var(--accent);
	}
	.record-side {
		display: flex;
		align-items: center;
		gap: 8px;
		flex: none;
	}
	.record-count {
		font-size: 10px;
		font-weight: 700;
		color: var(--accent);
		background: var(--accent-soft);
		border-radius: 999px;
		padding: 2px 7px;
		font-variant-numeric: tabular-nums;
	}
	.record-time {
		flex: none;
		font-size: 10px;
		color: var(--muted);
	}
	.redact-toggle {
		display: flex;
		align-items: center;
		gap: 5px;
		font-size: 11px;
		color: var(--muted);
		white-space: nowrap;
	}
	.redact-actions {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		margin-top: 14px;
	}
	@media (max-width: 720px) {
		.rule-pattern {
			flex-basis: calc(100% - 34px);
		}
		.redact-actions {
			flex-wrap: wrap;
		}
	}
</style>
