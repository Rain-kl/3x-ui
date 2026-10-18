import { useEffect, useRef, useState } from 'react';
import { FormProvider } from 'react-hook-form';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Badge,
  Button,
  Card,
  Col,
  Empty,
  Form,
  Image,
  Input,
  List,
  Pagination,
  Row,
  Space,
  Tag,
  Typography,
  Upload,
  message,
} from 'antd';
import { PaperClipOutlined, SendOutlined } from '@ant-design/icons';
import { FormField, useZodForm } from '@/components/form/rhf';
import { telegramApi, telegramMediaURL, TelegramComposerSchema, type TelegramMessage } from './api';

interface Props {
  tgId: number | null;
  onSelect: (tgId: number) => void;
}

function Attachment({ row }: { row: TelegramMessage }) {
  if (
    ![
      'photo',
      'document',
      'video',
      'audio',
      'voice',
      'sticker',
      'animation',
      'video_note',
    ].includes(row.kind)
  )
    return null;
  const url = telegramMediaURL(row.id);
  return (
    <Space orientation="vertical">
      {(row.kind === 'photo' || (row.kind === 'sticker' && row.fileName.endsWith('.webp'))) && (
        <Image loading="lazy" src={url} width={160} />
      )}
      {['video', 'video_note', 'animation'].includes(row.kind) && (
        // oxlint-disable-next-line jsx-a11y/media-has-caption -- Telegram attachments do not supply caption tracks; message text and downloads remain available.
        <video controls preload="none" src={url} style={{ maxWidth: '100%', maxHeight: 260 }} />
      )}
      {['audio', 'voice'].includes(row.kind) && (
        // oxlint-disable-next-line jsx-a11y/media-has-caption -- Telegram voice messages do not supply transcripts; the original file remains downloadable.
        <audio controls preload="none" src={url} />
      )}
      <a href={url} target="_blank" rel="noopener noreferrer">
        {row.fileName || row.kind} · {(row.fileSize / 1024).toFixed(1)} KiB
      </a>
    </Space>
  );
}

function Thread({ tgId }: { tgId: number }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [reply, setReply] = useState<TelegramMessage | null>(null);
  const [uploading, setUploading] = useState(false);
  const [messageApi, messageHolder] = message.useMessage();
  const methods = useZodForm(TelegramComposerSchema, { defaultValues: { text: '' } });
  const query = useQuery({
    queryKey: ['telegram', 'messages', tgId, page],
    queryFn: () => telegramApi.messages(tgId, page),
    refetchInterval: 3000,
  });
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['telegram'] });
  const send = useMutation({
    mutationFn: (text: string) => telegramApi.send(tgId, text, reply?.id ?? 0),
  });
  const bottom = useRef<HTMLDivElement>(null);
  const latestId = query.data?.items[0]?.id;
  useEffect(() => {
    if (page === 1) bottom.current?.scrollIntoView({ block: 'nearest' });
  }, [latestId, page]);
  useEffect(() => {
    if (!latestId || page !== 1) return;
    void telegramApi
      .read(tgId, latestId)
      .then(() => queryClient.invalidateQueries({ queryKey: ['telegram', 'conversations'] }))
      .catch((error: unknown) =>
        messageApi.error(error instanceof Error ? error.message : t('somethingWentWrong')),
      );
  }, [latestId, tgId, queryClient, page, messageApi, t]);
  const sendText = methods.handleSubmit(async ({ text }) => {
    try {
      await send.mutateAsync(text);
      methods.reset({ text: '' });
      setReply(null);
      setPage(1);
      await invalidate();
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : t('somethingWentWrong'));
    }
  });
  return (
    <Card size="small" title={`${t('pages.telegram.conversation')} · ${tgId}`}>
      {messageHolder}
      {query.error && <Alert type="error" title={query.error.message} />}
      <div style={{ height: 'clamp(320px, 50vh, 620px)', overflowY: 'auto', padding: 8 }}>
        {(query.data?.items ?? [])
          .slice()
          .reverse()
          .map((row) => (
            <div
              key={row.id}
              style={{
                display: 'flex',
                justifyContent: row.direction === 'outgoing' ? 'flex-end' : 'flex-start',
                marginBottom: 12,
              }}
            >
              <Card
                size="small"
                style={{
                  maxWidth: '90%',
                  background:
                    row.direction === 'outgoing' ? 'var(--ant-color-primary-bg)' : undefined,
                }}
              >
                <Space size={4} wrap>
                  <Tag>
                    {row.direction === 'incoming'
                      ? t('pages.telegram.user')
                      : t(`pages.telegram.roles.${row.role}`, { defaultValue: row.role })}
                  </Tag>
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    {new Date(row.createdAt).toLocaleString()}
                  </Typography.Text>
                  <Typography.Text type={row.status === 'failed' ? 'danger' : 'secondary'}>
                    {t(`pages.telegram.delivery.${row.status}`, { defaultValue: row.status })}
                  </Typography.Text>
                  <Button size="small" type="text" onClick={() => setReply(row)}>
                    {t('pages.telegram.reply')}
                  </Button>
                </Space>
                {row.replyId > 0 && (
                  <Typography.Text type="secondary">↪ #{row.replyId}</Typography.Text>
                )}
                {row.text && (
                  <div style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', marginTop: 6 }}>
                    {row.text}
                  </div>
                )}
                <Attachment row={row} />
                {row.error && <Alert type="error" title={row.error} style={{ marginTop: 8 }} />}
              </Card>
            </div>
          ))}
        {query.data?.items.length === 0 && <Empty />}
        <div ref={bottom} />
      </div>
      <Pagination
        size="small"
        current={page}
        pageSize={50}
        total={query.data?.total ?? 0}
        onChange={setPage}
        showSizeChanger={false}
      />
      {reply && (
        <Alert
          type="info"
          closable
          onClose={() => setReply(null)}
          title={`${t('pages.telegram.reply')} #${reply.id}: ${reply.text.slice(0, 100)}`}
          style={{ marginTop: 12 }}
        />
      )}
      <FormProvider {...methods}>
        <Form component="div" layout="vertical" style={{ marginTop: 12 }}>
          <FormField name="text">
            <Input.TextArea
              autoSize={{ minRows: 2, maxRows: 6 }}
              maxLength={3500}
              showCount
              placeholder={t('pages.telegram.messagePlaceholder')}
              onPressEnter={(event) => {
                if ((event.ctrlKey || event.metaKey) && !send.isPending) void sendText();
              }}
            />
          </FormField>
          <Space wrap>
            <Upload
              showUploadList={false}
              disabled={uploading || send.isPending}
              beforeUpload={(file) => {
                const caption = methods.getValues('text').trim();
                if (file.size > 20 * 1024 * 1024 || caption.length > 900) {
                  messageApi.error(t('pages.telegram.uploadLimit'));
                  return false;
                }
                const body = new FormData();
                body.append('file', file);
                body.append('tgId', String(tgId));
                body.append('caption', caption);
                body.append('replyId', String(reply?.id ?? 0));
                setUploading(true);
                void telegramApi
                  .upload(body)
                  .then(async () => {
                    methods.reset({ text: '' });
                    setReply(null);
                    setPage(1);
                    await invalidate();
                  })
                  .catch((error: unknown) =>
                    messageApi.error(
                      error instanceof Error ? error.message : t('somethingWentWrong'),
                    ),
                  )
                  .finally(() => setUploading(false));
                return false;
              }}
            >
              <Button icon={<PaperClipOutlined />} loading={uploading}>
                {t('pages.telegram.attach')}
              </Button>
            </Upload>
            <Button
              type="primary"
              icon={<SendOutlined />}
              loading={send.isPending}
              disabled={uploading}
              onClick={() => void sendText()}
            >
              {t('pages.telegram.send')}
            </Button>
            <Typography.Text type="secondary">{t('pages.telegram.queueHelp')}</Typography.Text>
          </Space>
        </Form>
      </FormProvider>
    </Card>
  );
}

export default function TelegramMessages({ tgId, onSelect }: Props) {
  const { t } = useTranslation();
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState('');
  const query = useQuery({
    queryKey: ['telegram', 'conversations', page, search],
    queryFn: () => telegramApi.conversations(page, search),
    refetchInterval: 5000,
  });
  return (
    <Row gutter={[16, 16]}>
      <Col xs={24} lg={8}>
        <Card size="small" title={t('pages.telegram.conversations')}>
          <Input.Search
            allowClear
            placeholder={t('search')}
            onSearch={(value) => {
              setSearch(value);
              setPage(1);
            }}
          />
          {query.error && <Alert type="error" title={query.error.message} />}
          <div style={{ maxHeight: '60vh', overflowY: 'auto' }}>
            <List
              loading={query.isLoading}
              dataSource={query.data?.items ?? []}
              renderItem={(row) => (
                <List.Item>
                  <Button
                    block
                    type={tgId === row.tgId ? 'primary' : 'text'}
                    style={{ height: 'auto', textAlign: 'start', padding: '10px 8px' }}
                    onClick={() => onSelect(row.tgId)}
                  >
                    <Space
                      orientation="vertical"
                      size={2}
                      style={{ width: '100%', alignItems: 'flex-start' }}
                    >
                      <Space>
                        <Badge count={row.unread} />
                        <span>{row.clientEmail || row.name || row.username || row.tgId}</span>
                      </Space>
                      <Typography.Text type="secondary">
                        {row.username ? `@${row.username} · ` : ''}
                        {row.tgId}
                      </Typography.Text>
                      <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                        {new Date(row.lastMessageAt).toLocaleString()}
                      </Typography.Text>
                    </Space>
                  </Button>
                </List.Item>
              )}
            />
          </div>
          <Pagination
            size="small"
            current={page}
            pageSize={20}
            total={query.data?.total ?? 0}
            onChange={setPage}
            showSizeChanger={false}
          />
        </Card>
      </Col>
      <Col xs={24} lg={16}>
        {tgId ? (
          <Thread key={tgId} tgId={tgId} />
        ) : (
          <Card>
            <Empty description={t('pages.telegram.selectConversation')} />
          </Card>
        )}
      </Col>
    </Row>
  );
}
