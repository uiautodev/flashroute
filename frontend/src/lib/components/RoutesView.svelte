<script>
	import { onMount } from 'svelte';
	import { api } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import { toast } from '#lib/toast.js';
	import Icon from './Icon.svelte';
	import Modal from './Modal.svelte';

	let items = [];
	let providers = [];
	let loading = true;
	let error = '';
	let showModal = false;
	let editing = false;
	let saving = false;
	let testingAlias = '';
	let testResults = {};
	let form = blankForm();

	onMount(load);

	function blankForm() {
		return { id: '', alias: '', note: '', enabled: true, targets: [] };
	}

	async function load() {
		loading = true;
		error = '';
		try {
			const [routeData, providerData] = await Promise.all([api.get('/api/routes'), api.get('/api/providers')]);
			items = routeData.routes || [];
			providers = providerData.providers || [];
		} catch (err) {
			error = err.message;
		} finally {
			loading = false;
		}
	}

	function providerName(id) {
		return providers.find((p) => p.id === id)?.name || id;
	}

	function providerModels(id) {
		return providers.find((p) => p.id === id)?.models || [];
	}

	function openAdd() {
		editing = false;
		form = blankForm();
		if (providers.length) {
			const provider = providers.find((p) => p.enabled) || providers[0];
			form.targets = [{ provider_id: provider.id, model: provider.models?.[0]?.id || '' }];
		}
		showModal = true;
	}

	function openEdit(item) {
		editing = true;
		form = {
			...item.route,
			targets: (item.route.targets || []).map((t) => ({ ...t }))
		};
		showModal = true;
	}

	function addTarget() {
		const provider = providers.find((p) => p.enabled) || providers[0];
		form.targets = [
			...form.targets,
			{ provider_id: provider?.id || '', model: provider?.models?.[0]?.id || '' }
		];
	}

	function removeTarget(index) {
		form.targets = form.targets.filter((_, i) => i !== index);
	}

	function moveTarget(index, direction) {
		const next = [...form.targets];
		const target = index + direction;
		if (target < 0 || target >= next.length) return;
		[next[index], next[target]] = [next[target], next[index]];
		form.targets = next;
	}

	function onProviderChange(index) {
		const provider = providers.find((p) => p.id === form.targets[index].provider_id);
		if (provider?.models?.length) form.targets[index].model = provider.models[0].id;
		else form.targets[index].model = '';
		form.targets = [...form.targets];
	}

	async function save() {
		if (form.targets.length === 0) {
			toast($t('routes.selectAtLeast'), 'error');
			return;
		}
		saving = true;
		try {
			await api.post('/api/routes', form);
			showModal = false;
			toast($t('common.save'), 'success');
			await load();
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			saving = false;
		}
	}

	async function toggleRoute(item) {
		try {
			await api.post('/api/routes', { ...item.route, enabled: !item.route.enabled });
			await load();
		} catch (err) {
			toast(err.message, 'error');
		}
	}

	async function remove(item) {
		if (!confirm($t('routes.deleteConfirm', { alias: item.route.alias }))) return;
		try {
			await api.del('/api/routes/' + item.route.id);
			toast($t('common.delete'), 'success');
			await load();
		} catch (err) {
			toast(err.message, 'error');
		}
	}

	async function testRoute(item) {
		const alias = item.route.alias;
		testingAlias = alias;
		try {
			const result = await api.post('/api/routes/test', { alias });
			testResults = { ...testResults, [alias]: result };
			if (result.ok) toast($t('routes.answer', { output: result.output }), 'success');
			else toast(result.error || $t('providers.testBad'), 'error', 5000);
		} catch (err) {
			testResults = { ...testResults, [alias]: { ok: false, error: err.message } };
			toast(err.message, 'error', 5000);
		} finally {
			testingAlias = '';
		}
	}

	function statFor(stats, target) {
		return (stats?.targets || []).find((s) => s.provider === target.provider_id && s.model === target.model) || {
			attempts: 0,
			success: 0,
			errors: 0,
			rate_limited: 0,
			hit_rate: 0,
			success_rate: 0
		};
	}

	function pct(value) {
		return `${Number(value || 0).toFixed(1)}%`;
	}
</script>

<div class="page-toolbar">
	<div class="row">
		<button class="btn btn-icon" onclick={load} title={$t('common.refresh')}><Icon name="refresh" size={15} /></button>
		<button class="btn btn-primary" onclick={openAdd}><Icon name="plus" size={15} />{$t('routes.add')}</button>
	</div>
</div>

{#if loading}
	<div class="grid">
		<div class="card card-pad"><div class="skeleton" style="width:30%"></div></div>
		<div class="card card-pad"><div class="skeleton" style="width:45%"></div></div>
	</div>
{:else if error}
	<div class="empty"><span class="empty-text">{error}</span><button class="btn" onclick={load}>{$t('common.refresh')}</button></div>
{:else if items.length === 0}
	<div class="empty">
		<div class="empty-icon"><Icon name="route" size={23} /></div>
		<h2 class="empty-title">{$t('routes.emptyTitle')}</h2>
		<p class="empty-text">{$t('routes.emptyBody')}</p>
		<button class="btn btn-primary" onclick={openAdd}><Icon name="plus" size={15} />{$t('routes.add')}</button>
	</div>
{:else}
	<div class="route-list">
		{#each items as item (item.route.id)}
			{@const route = item.route}
			{@const stats = item.stats || { requests: 0, success: 0, failure: 0, success_rate: 0, targets: [] }}
			<article class="card route-card" class:disabled={!route.enabled}>
				<header class="route-head">
					<div class="route-title-wrap">
						<div class="alias-row">
							<code class="alias">{route.alias}</code>
							<span class="badge {route.enabled ? 'badge-ok' : 'badge-off'}">
								{route.enabled ? $t('common.enabled') : $t('common.disabled')}
							</span>
						</div>
						{#if route.note}<p class="route-note">{route.note}</p>{/if}
					</div>
					<div class="row">
						<button class="switch" class:on={route.enabled} onclick={() => toggleRoute(item)} aria-label={$t('common.enabled')}></button>
						<button class="btn btn-sm" onclick={() => testRoute(item)} disabled={testingAlias === route.alias}>
							{#if testingAlias === route.alias}<span class="spinner"></span>{:else}<Icon name="zap" size={13} />{/if}
							{$t('routes.testRoute')}
						</button>
						<button class="btn btn-ghost btn-icon" onclick={() => openEdit(item)} title={$t('common.edit')}><Icon name="edit" size={15} /></button>
						<button class="btn btn-ghost btn-icon" onclick={() => remove(item)} title={$t('common.delete')}><Icon name="trash" size={15} /></button>
					</div>
				</header>

				<div class="metrics">
					<div class="metric">
						<span class="metric-label">{$t('routes.requests')}</span>
						<span class="metric-value">{stats.requests || 0}</span>
					</div>
					<div class="metric">
						<span class="metric-label">{$t('routes.successRate')}</span>
						<span class="metric-value">{pct(stats.success_rate)}</span>
					</div>
					<div class="metric">
						<span class="metric-label">{$t('routes.hits')}</span>
						<span class="metric-value">{route.targets.length}</span>
					</div>
					<div class="metric wide">
						<span class="metric-label">{$t('routes.successRate')} / {$t('routes.requests')}</span>
						<div class="progress"><span style={`width:${Math.min(stats.success_rate || 0, 100)}%`}></span></div>
					</div>
				</div>

				<div class="targets">
					<div class="targets-head">
						<span class="detail-label">{$t('routes.targets')}</span>
						<span class="hint">{$t('routes.targetOrderHint')}</span>
					</div>
					{#if route.targets.length === 0}
						<div class="hint">{$t('routes.noStats')}</div>
					{/if}
					{#each route.targets as target, index}
						{@const st = statFor(stats, target)}
						<div class="target-row">
							<span class="target-index">{index + 1}</span>
							<div class="target-main">
								<div class="target-name">
									<span>{providerName(target.provider_id)}</span>
									<code class="mono">{target.model}</code>
								</div>
								<div class="target-meta">
									<span>{$t('routes.attempts')} {st.attempts}</span>
									<span>{$t('routes.hits')} {st.success}</span>
									<span>{$t('routes.rateLimited')} {st.rate_limited}</span>
									<span>{$t('routes.errors')} {st.errors}</span>
								</div>
							</div>
							<div class="hit">
								<span class="hit-value">{pct(st.hit_rate)}</span>
								<span class="hit-label">{$t('routes.hitRate')}</span>
								<div class="progress"><span style={`width:${Math.min(st.hit_rate || 0, 100)}%`}></span></div>
							</div>
						</div>
					{/each}
				</div>

				{#if testResults[route.alias]}
					{@const result = testResults[route.alias]}
					<div class="route-test" class:bad={!result.ok}>
						{#if result.ok}
							<Icon name="check" size={15} />
							<div>
								<strong>{$t('routes.testResult')}</strong>
								<span class="mono">{result.provider_id}/{result.model}</span>
								<span>{result.latency_ms} ms</span>
								<div class="test-output">{$t('routes.answer', { output: result.output })}</div>
							</div>
						{:else}
							<Icon name="alert" size={15} />
							<div><strong>{$t('routes.testResult')}</strong> · {result.error}</div>
						{/if}
						<button
							class="btn btn-ghost btn-icon"
							onclick={() => {
								const next = { ...testResults };
								delete next[route.alias];
								testResults = next;
							}}
						>
							<Icon name="x" size={14} />
						</button>
					</div>
				{/if}
			</article>
		{/each}
	</div>
{/if}

{#if showModal}
	<Modal title={editing ? $t('routes.edit') : $t('routes.add')} onClose={() => (showModal = false)}>
		<div class="form-grid">
			<div class="field">
				<label for="route-alias">{$t('routes.alias')}<span class="required-mark" aria-hidden="true">*</span></label>
				<input id="route-alias" class="input mono" bind:value={form.alias} disabled={editing} placeholder="grounder" />
				<span class="hint">{$t('routes.aliasHint')}</span>
			</div>
			<div class="field">
				<label for="route-note">{$t('routes.note')}</label>
				<input id="route-note" class="input" bind:value={form.note} placeholder={$t('routes.notePlaceholder')} />
			</div>
			<div class="field full">
				<div class="spread">
					<div>
						<span class="field-label">{$t('routes.targets')}<span class="required-mark" aria-hidden="true">*</span></span>
						<p class="hint" style="margin-top:4px">{$t('routes.targetOrderHint')}</p>
					</div>
					<button class="btn btn-sm" onclick={addTarget}><Icon name="plus" size={13} />{$t('routes.addTarget')}</button>
				</div>
				<div class="target-editor">
					{#each form.targets as target, index}
						<div class="target-edit-row">
							<span class="order">{index + 1}</span>
							<select class="select" bind:value={target.provider_id} onchange={() => onProviderChange(index)}>
								<option value="">{$t('routes.selectProvider')}</option>
								{#each providers as provider}
									<option value={provider.id}>{provider.name} ({provider.id})</option>
								{/each}
							</select>
							<input
								class="input mono"
								bind:value={target.model}
								list={`models-${index}`}
								placeholder={$t('routes.modelPlaceholder')}
							/>
							<datalist id={`models-${index}`}>
								{#each providerModels(target.provider_id) as model}
									<option value={model.id}>{model.name || model.id}</option>
								{/each}
							</datalist>
							<div class="target-buttons">
								<button class="btn btn-ghost btn-icon btn-sm" onclick={() => moveTarget(index, -1)} disabled={index === 0} title={$t('common.moveUp')}>
									<Icon name="arrow-up" size={13} />
								</button>
								<button
									class="btn btn-ghost btn-icon btn-sm"
									onclick={() => moveTarget(index, 1)}
									disabled={index === form.targets.length - 1}
									title={$t('common.moveDown')}
								>
									<Icon name="arrow-down" size={13} />
								</button>
								<button class="btn btn-ghost btn-icon btn-sm" onclick={() => removeTarget(index)} title={$t('common.delete')}>
									<Icon name="trash" size={13} />
								</button>
							</div>
						</div>
					{/each}
					{#if form.targets.length === 0}
						<div class="hint">{$t('routes.selectAtLeast')}</div>
					{/if}
				</div>
			</div>
			<div class="spread full">
				<div class="row">
					<button class="switch" class:on={form.enabled} onclick={() => (form.enabled = !form.enabled)} aria-label={$t('common.enabled')}></button>
					<span class="hint">{$t('common.enabled')}</span>
				</div>
			</div>
		</div>
		<div slot="footer" class="modal-foot">
			<button class="btn" onclick={() => (showModal = false)}>{$t('common.cancel')}</button>
			<button class="btn btn-primary" onclick={save} disabled={saving || !form.alias || form.targets.length === 0}>
				{#if saving}<span class="spinner"></span>{/if}
				{$t('common.save')}
			</button>
		</div>
	</Modal>
{/if}

<style>
	.route-list {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.route-card {
		padding: 18px;
		display: flex;
		flex-direction: column;
		gap: 15px;
	}
	.route-card.disabled {
		opacity: 0.72;
	}
	.route-head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
	}
	.alias-row {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.alias {
		font-size: 15px;
		font-weight: 700;
		color: var(--accent);
		background: var(--accent-soft);
		border: 1px solid var(--accent-border-soft);
		border-radius: 7px;
		padding: 3px 8px;
	}
	.route-note {
		font-size: 11.5px;
		color: var(--muted);
		margin-top: 6px;
	}
	.metrics {
		display: grid;
		grid-template-columns: repeat(3, 110px) minmax(140px, 1fr);
		gap: 10px;
		padding: 11px 13px;
		border: 1px solid var(--line-soft);
		border-radius: 10px;
		background: var(--surface-tint);
	}
	.metric {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.metric.wide {
		justify-content: center;
		gap: 7px;
	}
	.metric-label {
		font-size: 9.5px;
		text-transform: uppercase;
		letter-spacing: 0.07em;
		color: var(--muted);
		font-weight: 700;
	}
	.metric-value {
		font-size: 17px;
		font-weight: 700;
	}
	.targets-head {
		display: flex;
		align-items: baseline;
		gap: 10px;
		flex-wrap: wrap;
		margin-bottom: 8px;
	}
	.targets {
		display: flex;
		flex-direction: column;
	}
	.target-row {
		display: grid;
		grid-template-columns: 24px minmax(0, 1fr) 150px;
		gap: 12px;
		align-items: center;
		padding: 9px 0;
		border-top: 1px solid var(--line-soft);
	}
	.target-index {
		width: 21px;
		height: 21px;
		border-radius: 6px;
		display: grid;
		place-items: center;
		background: var(--panel-3);
		color: var(--muted);
		font-size: 10px;
		font-weight: 700;
	}
	.target-main {
		min-width: 0;
	}
	.target-name {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
		font-size: 12px;
		color: var(--text-soft);
	}
	.target-name code {
		font-size: 10.5px;
		color: var(--muted);
	}
	.target-meta {
		display: flex;
		gap: 10px;
		flex-wrap: wrap;
		font-size: 9.5px;
		color: var(--muted);
		margin-top: 3px;
	}
	.hit {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 2px 6px;
		align-items: baseline;
	}
	.hit-value {
		font-size: 13px;
		font-weight: 700;
		text-align: right;
	}
	.hit-label {
		font-size: 9px;
		color: var(--muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.hit .progress {
		grid-column: 1 / -1;
	}
	.route-test {
		display: flex;
		align-items: flex-start;
		gap: 9px;
		padding: 10px 12px;
		border: 1px solid var(--accent-border);
		background: var(--accent-soft);
		color: var(--accent);
		border-radius: 9px;
		font-size: 11.5px;
	}
	.route-test.bad {
		border-color: rgba(251, 113, 133, 0.25);
		background: var(--danger-soft);
		color: var(--danger-text);
	}
	.route-test > div {
		flex: 1;
		display: flex;
		gap: 7px;
		flex-wrap: wrap;
		line-height: 1.5;
	}
	.route-test .mono {
		font-size: 10.5px;
		opacity: 0.8;
	}
	.test-output {
		width: 100%;
		color: var(--text-soft);
		font-size: 11px;
		white-space: pre-wrap;
	}
	.target-editor {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin-top: 8px;
	}
	.target-edit-row {
		display: grid;
		grid-template-columns: 24px 190px minmax(0, 1fr) auto;
		gap: 7px;
		align-items: center;
	}
	.order {
		width: 21px;
		height: 21px;
		border-radius: 6px;
		display: grid;
		place-items: center;
		background: var(--panel-3);
		color: var(--muted);
		font-size: 10px;
	}
	.target-buttons {
		display: flex;
		gap: 2px;
	}
	@media (max-width: 760px) {
		.metrics {
			grid-template-columns: repeat(3, 1fr);
		}
		.metric.wide {
			grid-column: 1 / -1;
		}
		.target-row {
			grid-template-columns: 24px minmax(0, 1fr);
		}
		.hit {
			grid-column: 2;
			width: 140px;
		}
		.target-edit-row {
			grid-template-columns: 24px minmax(0, 1fr) auto;
		}
		.target-edit-row .select {
			grid-column: 2;
		}
		.target-edit-row .input {
			grid-column: 2;
		}
	}
</style>
