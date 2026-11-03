<script>
	import { confirmState, resolveConfirm } from '#lib/confirm.js';
	import { t } from '#lib/i18n.js';
	import Modal from './Modal.svelte';
</script>

{#if $confirmState}
	<Modal title={$confirmState.title || $t('common.confirm')} onClose={() => resolveConfirm(false)}>
		<p class="confirm-message">{$confirmState.message}</p>
		<div slot="footer" class="modal-foot">
			<button class="btn" onclick={() => resolveConfirm(false)}>
				{$confirmState.cancelLabel || $t('common.cancel')}
			</button>
			<button class="btn {$confirmState.danger ? 'btn-danger' : 'btn-primary'}" onclick={() => resolveConfirm(true)}>
				{$confirmState.confirmLabel || $t('common.confirm')}
			</button>
		</div>
	</Modal>
{/if}

<style>
	.confirm-message {
		margin: 0;
		font-size: 12.5px;
		line-height: 1.65;
		color: var(--text-soft);
	}
</style>
