#! /usr/bin/env ruby
# frozen_string_literal: true

require 'net/http'
require 'sensu-plugin/check/cli'

class CheckNginxStatus < Sensu::Plugin::Check::CLI
  option :hostname,
         short: '-h HOSTNAME',
         long: '--host HOSTNAME',
         description: 'Nginx hostname',
         default: '127.0.0.1'

  option :port,
         short: '-p PORT',
         long: '--port PORT',
         description: 'Nginx port',
         proc: proc(&:to_i),
         default: 80

  option :path,
         short: '-q STATUSPATH',
         long: '--path STATUSPATH',
         description: 'Path to your stub status module',
         default: 'nginx-status'

  option :activecon,
         short: '-a ACTIVE_CONNECTION_THRESHOLD',
         long: '--activecon ACTIVE_CONNECTION_THRESHOLD',
         description: 'Active connections threshold',
         proc: proc(&:to_i),
         default: 300

  option :waitingreq,
         short: '-w WAITING_REQUESTS_THRESHOLD',
         long: '--waitingreq WAITING_REQUESTS_THRESHOLD',
         description: 'Waiting requests threshold',
         proc: proc(&:to_i),
         default: 30

  def run
    response = Net::HTTP.start(config[:hostname], config[:port]) do |connection|
      request = Net::HTTP::Get.new("/#{config[:path]}")
      connection.request(request)
    end

    response.body.split(/\r?\n/).each do |line|
      if line =~ /^Active connections:\s+(\d+)/
        connections = line.match(/^Active connections:\s+(\d+)/).to_a
        warning "Active connections: #{connections[1]}" if connections[1].to_i > config[:activecon]
      end

      if line =~ /^Reading:\s+(\d+).*Writing:\s+(\d+).*Waiting:\s+(\d+)/
        queues = line.match(/^Reading:\s+(\d+).*Writing:\s+(\d+).*Waiting:\s+(\d+)/).to_a
        warning "Waiting requests: #{queues[3]}" if queues[3].to_i > config[:waitingreq]
      end
    end

    ok 'Nginx is Alive and healthy' if response.code == '200'
    warning 'Nginx Status endpoint is mis-configured' if response.code == '301'
    critical 'Nginx is Down'
  rescue StandardError => e
    unknown "Could not fetch Nginx status | #{e.message}"
  end
end
