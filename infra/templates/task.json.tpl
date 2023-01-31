[
  {
    "name": "${app_name}",
    "image": "${app_image}",
    "cpu": ${app_cpu},
    "memory": ${app_memory},
    "essential": true,
    "logConfiguration": {
      "logDriver": "awslogs",
      "options": {
        "awslogs-group": "${log_group_name}",
        "awslogs-region": "${region}",
        "awslogs-stream-prefix": "ecs"
      }
    },
    "environment": [
      {
        "name": "AWS_REGION",
        "value": "${region}"
      },
      {
        "name": "LOCK_TABLE_NAME",
        "value": "${lock_table_name}"
      }
    ],
    "secrets": [
      {
        "name": "DISCORD_APPLICATION_ID",
        "valueFrom": "${discord_application_id}"
      },
      {
        "name": "DISCORD_PUBLIC_KEY",
        "valueFrom": "${discord_public_key}"
      },
      {
        "name": "DISCORD_TOKEN",
        "valueFrom": "${discord_token}"
      },
      {
        "name": "DISCORD_GUILD_ID",
        "valueFrom": "${discord_guild_id}"
      },
      {
        "name": "OPENAI_TOKEN",
        "valueFrom": "${openai_token}"
      }
    ]
  }
]
